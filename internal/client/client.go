// Package client talks to a tend server over a socket.
//
// A Client is the other half of internal/proto: it turns method calls into
// frames and matches replies back to the caller, so nothing above it deals
// with the wire.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/transport"
)

// ErrClosed is returned once the connection is gone.
var ErrClosed = errors.New("client: connection closed")

// callTimeout bounds a request. A server that stops answering must surface as
// a failed call rather than a hung client.
const callTimeout = 30 * time.Second

// Handler receives what the server sends unsolicited. Every method is called
// from the client's reader goroutine, so none may block for long or call back
// into the same client.
type Handler interface {
	// Disconnected reports that the connection ended. It is how a client
	// learns to stop drawing a session it can no longer see, and when to
	// reconnect.
	Disconnected()

	// Event reports something that happened in the session.
	Event(proto.Event)
	// PaneOutput reports a pane's current screen. data is only valid for the
	// duration of the call.
	PaneOutput(pane uint64, data []byte)
}

// Client is a connection to a tend server.
type Client struct {
	conn    *proto.Conn
	handler Handler

	nextID atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan proto.Response
	closed  bool
	closeWG sync.WaitGroup

	server proto.HelloResult
}

// Dial connects to a session's socket and completes the handshake.
//
// The handshake is not optional: a version mismatch has to be reported as
// itself, or it shows up later as a message the other side cannot parse.
func Dial(socketPath string, handler Handler) (*Client, error) {
	nc, err := transport.Dial(socketPath)
	if err != nil {
		return nil, err
	}
	return Attach(nc, handler)
}

// DialRemote reaches a session on another machine through the command that
// carries it, and completes the handshake.
//
// When the handshake fails, what the command said on stderr is the reason and
// is put in the error: "tend: command not found" or a refused host key is what
// the user needs, and "connection closed" is what they would otherwise get.
func DialRemote(argv []string, handler Handler) (*Client, error) {
	rc, err := transport.Remote(argv)
	if err != nil {
		return nil, err
	}
	c, err := Attach(rc, handler)
	if err != nil {
		_ = rc.Close()
		if why := rc.Complaint(); why != "" {
			return nil, fmt.Errorf("%w: %s", err, why)
		}
		return nil, err
	}
	return c, nil
}

// Attach completes the handshake over a connection that is already open.
func Attach(nc io.ReadWriteCloser, handler Handler) (*Client, error) {
	if handler == nil {
		handler = nopHandler{}
	}
	c := &Client{
		conn:    proto.NewConn(nc),
		handler: handler,
		pending: make(map[uint64]chan proto.Response),
	}

	c.closeWG.Add(1)
	go c.readLoop()

	var hello proto.HelloResult
	err := c.Call(proto.MethodHello, proto.HelloParams{
		Version: proto.Version,
		Client:  "tend",
	}, &hello)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("client: handshake: %w", err)
	}
	c.server = hello
	return c, nil
}

// Server returns what the server said about itself during the handshake.
func (c *Client) Server() proto.HelloResult { return c.server }

// Supports reports whether the server advertised a method.
//
// A client should disable an action the server cannot perform rather than let
// a user try it and receive an error, which is the difference between a
// feature being absent and being broken.
func (c *Client) Supports(method string) bool {
	for _, m := range c.server.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// Call sends a request and waits for its answer. result may be nil.
func (c *Client) Call(method string, params, result any) error {
	id := c.nextID.Add(1)

	req := proto.Request{ID: id, Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("client: encoding params for %s: %w", method, err)
		}
		req.Params = encoded
	}

	reply := make(chan proto.Response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.pending[id] = reply
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.conn.WriteJSON(proto.FrameRequest, req); err != nil {
		return fmt.Errorf("client: sending %s: %w", method, err)
	}

	select {
	case resp := <-reply:
		if resp.Error != "" {
			if strings.Contains(resp.Error, proto.ErrUnknownMethod.Error()) {
				// Carried across the wire as text and recognised here, so a
				// caller can say what it means rather than repeating it.
				return fmt.Errorf("%s: %w", method, proto.ErrUnknownMethod)
			}
			return fmt.Errorf("%s: %s", method, resp.Error)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("client: decoding %s result: %w", method, err)
		}
		return nil
	case <-time.After(callTimeout):
		return fmt.Errorf("client: %s timed out", method)
	}
}

// SendInput sends keystrokes to a pane. It does not wait for an answer: input
// is a stream, and a round trip per keystroke would be felt.
func (c *Client) SendInput(pane uint64, data []byte) error {
	return c.conn.WritePaneBytes(proto.FrameInput, pane, data)
}

// Close ends the connection. Pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	err := c.conn.Close()
	c.closeWG.Wait()
	return err
}

// readLoop dispatches everything the server sends.
func (c *Client) readLoop() {
	defer c.closeWG.Done()
	defer c.handler.Disconnected()
	defer c.failPending()

	for {
		frame, err := c.conn.ReadFrame()
		if err != nil {
			return // including io.EOF: the server hung up
		}

		switch frame.Type {
		case proto.FrameResponse:
			var resp proto.Response
			if err := json.Unmarshal(frame.Payload, &resp); err != nil {
				continue
			}
			c.deliver(resp)

		case proto.FrameEvent:
			var ev proto.Event
			if err := json.Unmarshal(frame.Payload, &ev); err != nil {
				continue
			}
			c.handler.Event(ev)

		case proto.FrameOutput:
			pane, data, err := proto.DecodePaneBytes(frame.Payload)
			if err != nil {
				continue
			}
			c.handler.PaneOutput(pane, data)

		default:
			// A frame type this build does not know is ignored, so a newer
			// server can send one without breaking an older client.
			continue
		}
	}
}

func (c *Client) deliver(resp proto.Response) {
	c.mu.Lock()
	reply, ok := c.pending[resp.ID]
	c.mu.Unlock()
	if !ok {
		// A late answer to a call that already gave up. Dropping it is right:
		// the caller has moved on.
		return
	}
	select {
	case reply <- resp:
	default:
	}
}

// failPending wakes every waiting call when the connection ends, so a caller
// learns the server is gone instead of waiting out the timeout.
func (c *Client) failPending() {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[uint64]chan proto.Response)
	c.mu.Unlock()

	for id, reply := range pending {
		select {
		case reply <- proto.Response{ID: id, Error: ErrClosed.Error()}:
		default:
		}
	}
}

type nopHandler struct{}

func (nopHandler) Event(proto.Event)         {}
func (nopHandler) PaneOutput(uint64, []byte) {}
func (nopHandler) Disconnected()             {}

// --- convenience wrappers --------------------------------------------------
//
// These exist so callers name a method once, here, rather than at every call
// site. A misspelled method string is otherwise a runtime error found by a
// user rather than a compile error found by the build.

// Snapshot returns the whole session.
func (c *Client) Snapshot() (proto.SessionSnapshot, error) {
	var out proto.SessionSnapshot
	return out, c.Call(proto.MethodSessionSnapshot, nil, &out)
}

// NewWorkspace adds a workspace.
func (c *Client) NewWorkspace(name string) (uint64, error) {
	return c.NewWorkspaceIn(name, "")
}

// NewWorkspaceIn adds a workspace rooted at a directory.
func (c *Client) NewWorkspaceIn(name, dir string) (uint64, error) {
	var out proto.WorkspaceNewResult
	err := c.Call(proto.MethodWorkspaceNew, proto.WorkspaceNewParams{Name: name, Dir: dir}, &out)
	return out.Workspace, err
}

// NewTab creates a tab with one pane.
func (c *Client) NewTab(workspace uint64, name string, spec proto.PaneSpec) (tab, pane uint64, err error) {
	var out proto.TabNewResult
	err = c.Call(proto.MethodTabNew, proto.TabNewParams{
		Workspace: workspace,
		Name:      name,
		Pane:      spec,
	}, &out)
	return out.Tab, out.Pane, err
}

// CloseTab closes a tab and every pane in it.
func (c *Client) CloseTab(tab uint64) error {
	return c.Call(proto.MethodTabClose, proto.TabCloseParams{Tab: tab}, nil)
}

// RenameTab changes a tab's label.
// GroupWorkspace moves a workspace into a group, or out of one when the name
// is empty.
func (c *Client) GroupWorkspace(ws uint64, group string) error {
	return c.Call(proto.MethodWorkspaceGroup, proto.WorkspaceGroupParams{Workspace: ws, Group: group}, nil)
}

// CloseWorkspace closes a workspace and every pane in it.
func (c *Client) CloseWorkspace(ws uint64) error {
	return c.Call(proto.MethodWorkspaceClose, proto.WorkspaceCloseParams{Workspace: ws}, nil)
}

func (c *Client) RenameTab(tab uint64, name string) error {
	return c.Call(proto.MethodTabRename, proto.TabRenameParams{Tab: tab, Name: name}, nil)
}

// RenameWorkspace changes a workspace's label.
func (c *Client) RenameWorkspace(ws uint64, name string) error {
	return c.Call(proto.MethodWorkspaceRename, proto.WorkspaceRenameParams{
		Workspace: ws, Name: name,
	}, nil)
}

// SplitPane divides a pane. direction is "columns" or "rows".
func (c *Client) SplitPane(target uint64, direction string, spec proto.PaneSpec) (uint64, error) {
	var out proto.PaneSplitResult
	err := c.Call(proto.MethodPaneSplit, proto.PaneSplitParams{
		Target:    target,
		Direction: direction,
		Pane:      spec,
	}, &out)
	return out.Pane, err
}

// ClosePane closes one pane.
func (c *Client) ClosePane(pane uint64) error {
	return c.Call(proto.MethodPaneClose, proto.PaneCloseParams{Pane: pane}, nil)
}

// ResizePane changes a pane's terminal size.
func (c *Client) ResizePane(pane uint64, cols, rows int) error {
	return c.Call(proto.MethodPaneResize, proto.PaneResizeParams{
		Pane: pane, Cols: cols, Rows: rows,
	}, nil)
}

// SubscribePanes replaces the set of panes whose screens this client receives.
func (c *Client) SubscribePanes(panes []uint64) error {
	return c.Call(proto.MethodPaneSubscribe, proto.PaneSubscribeParams{Panes: panes}, nil)
}

// PaneScreen fetches a pane's current screen.
func (c *Client) PaneScreen(pane uint64) (proto.PaneScreenResult, error) {
	return c.PaneScreenAt(pane, 0)
}

// PaneScreenAt fetches a pane as it was offset lines back through its history.
func (c *Client) PaneScreenAt(pane uint64, offset int) (proto.PaneScreenResult, error) {
	var out proto.PaneScreenResult
	return out, c.Call(proto.MethodPaneScreen, proto.PaneScreenParams{
		Pane: pane, Offset: offset,
	}, &out)
}

// AdjustSplit moves one edge of a pane. side is "left", "right", "up" or
// "down"; cols and rows are the size the tab is being drawn in.
// PaneText reads the text in a region of a pane, which may reach outside the
// view into the scrollback.
func (c *Client) PaneText(p proto.PaneTextParams) (string, error) {
	var out proto.PaneTextResult
	err := c.Call(proto.MethodPaneText, p, &out)
	return out.Text, err
}

func (c *Client) AdjustSplit(pane uint64, side string, cells, cols, rows int) error {
	return c.Call(proto.MethodPaneAdjust, proto.PaneAdjustParams{
		Target: pane, Side: side, Cells: cells, Cols: cols, Rows: rows,
	}, nil)
}

// TabLayout asks where a tab's panes go at the given size.
func (c *Client) TabLayout(tab uint64, cols, rows int) (proto.TabLayoutResult, error) {
	var out proto.TabLayoutResult
	return out, c.Call(proto.MethodTabLayout, proto.TabLayoutParams{
		Tab: tab, Cols: cols, Rows: rows,
	}, &out)
}

// CopyMotion asks where a copy-mode motion lands.
func (c *Client) CopyMotion(pane uint64, from proto.CopyPoint, motion string) (proto.PaneCopyResult, error) {
	var out proto.PaneCopyResult
	return out, c.Call(proto.MethodPaneCopyMotion, proto.PaneCopyMotionParams{
		Pane: pane, From: from, Motion: motion,
	}, &out)
}

// CopySearch asks where the nearest match of a query is.
func (c *Client) CopySearch(pane uint64, from proto.CopyPoint, query, direction string) (proto.PaneCopyResult, error) {
	var out proto.PaneCopyResult
	return out, c.Call(proto.MethodPaneCopySearch, proto.PaneCopySearchParams{
		Pane: pane, From: from, Query: query, Direction: direction,
	}, &out)
}

// SwapPaneToward exchanges a pane with its neighbour on a side, as laid out at
// cols by rows, and names the neighbour.
func (c *Client) SwapPaneToward(pane uint64, side string, cols, rows int) (uint64, error) {
	var out proto.PaneSwapResult
	err := c.Call(proto.MethodPaneSwap, proto.PaneSwapParams{
		Pane: pane, Side: side, Cols: cols, Rows: rows,
	}, &out)
	return out.Other, err
}

// SwapPanes exchanges two panes.
func (c *Client) SwapPanes(pane, target uint64) error {
	return c.Call(proto.MethodPaneSwap, proto.PaneSwapParams{Pane: pane, Target: target}, nil)
}

// MoveTab moves a tab delta places along its row.
func (c *Client) MoveTab(tab uint64, delta int) error {
	return c.Call(proto.MethodTabMove, proto.MoveParams{ID: tab, Delta: delta}, nil)
}

// MoveWorkspace moves a space delta places along the list.
func (c *Client) MoveWorkspace(ws uint64, delta int) error {
	return c.Call(proto.MethodWorkspaceMove, proto.MoveParams{ID: ws, Delta: delta}, nil)
}

// Handoff asks the server to replace itself with the binary now on disk,
// keeping what runs in its panes. The connection drops once it has.
func (c *Client) Handoff() error {
	return c.Call(proto.MethodServerHandoff, nil, nil)
}

// Shutdown asks the server to stop.
func (c *Client) Shutdown() error {
	return c.Call(proto.MethodServerShutdown, nil, nil)
}
