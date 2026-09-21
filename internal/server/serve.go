package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/vt"
)

// frameInterval paces how often a client is sent a pane's screen.
//
// Output events arrive once per read from the pty, which for a busy agent is
// far more often than anyone can look at. Pacing here bounds a client's
// traffic by time rather than by how noisy its panes are.
const frameInterval = 33 * time.Millisecond

// eventBuffer is how many events a connected client may fall behind by before
// it starts losing them. Generous, because falling behind costs a client a
// resynchronise; blocking would cost every agent its progress.
const eventBuffer = 256

// Methods lists what this server implements, so a client can disable an action
// it cannot perform rather than discovering the gap when a user tries it.
var Methods = []string{
	proto.MethodHello,
	proto.MethodSessionSnapshot,
	proto.MethodWorkspaceNew,
	proto.MethodTabNew,
	proto.MethodTabClose,
	proto.MethodWorkspaceClose,
	proto.MethodWorkspaceGroup,
	proto.MethodTabRename,
	proto.MethodWorkspaceRename,
	proto.MethodPaneSplit,
	proto.MethodPaneClose,
	proto.MethodPaneResize,
	proto.MethodPaneSubscribe,
	proto.MethodPaneScreen,
	proto.MethodPaneText,
	proto.MethodPaneAdjust,
	proto.MethodTabLayout,
	proto.MethodServerShutdown,
	proto.MethodServerHandoff,
	proto.MethodPaneCopyMotion,
	proto.MethodPaneCopySearch,
	proto.MethodServerReloadConfig,
	proto.MethodPaneFocus,
	proto.MethodPaneRename,
	proto.MethodPaneGraphics,
	proto.MethodPaneSwap,
	proto.MethodTabMove,
	proto.MethodWorkspaceMove,
}

// Serve accepts connections until the listener is closed.
//
// A connection that misbehaves is dropped on its own: one client sending
// nonsense must not disturb another, and neither must disturb the panes.
func (s *Server) Serve(ln net.Listener) error {
	// Accept only unblocks when the listener closes, so shutdown has to reach
	// it. Without this, stopping the server over the socket leaves Serve
	// waiting for a connection that will never come.
	go func() {
		<-s.done
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isClosed() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveConn(conn)
		}()
	}
}

// addConn registers a client, reporting false if the server is shutting down.
func (s *Server) addConn(c *clientConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) removeConn(c *clientConn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// clientConn is one connected client.
type clientConn struct {
	srv  *Server
	conn *proto.Conn

	// mu guards the set of panes whose output this client receives. Changing
	// it is rare; reading it happens on every pane read, so it is held for as
	// short a time as possible.
	mu         sync.RWMutex
	subscribed map[session.PaneID]bool

	// shutdown is set when this connection asked the server to stop. It is
	// acted on after the reply is written, not during the call: shutting down
	// first closes this very connection, and the client would see its request
	// fail rather than succeed.
	shutdown bool
	// handoff is set when this connection's request put a replacement in
	// charge, which this server now owes a commit.
	handoff *Handoff
}

func (s *Server) serveConn(nc net.Conn) {
	c := &clientConn{
		srv:        s,
		conn:       proto.NewConn(nc),
		subscribed: make(map[session.PaneID]bool),
	}
	defer c.conn.Close()

	if !s.addConn(c) {
		return // shutting down already
	}
	defer s.removeConn(c)

	sub := s.Subscribe(eventBuffer)
	defer sub.Close()

	// Events and pane output are pushed from here; requests are pulled by the
	// loop below. Both write through the same Conn, which serialises them.
	done := make(chan struct{})
	var pump sync.WaitGroup
	pump.Add(1)
	go func() {
		defer pump.Done()
		c.pumpEvents(sub, done)
	}()

	c.serveRequests()
	close(done)
	pump.Wait()
}

// pumpEvents forwards server events and pane screens to the client.
//
// Screens go out on a tick rather than on every output event. A pane
// redrawing continuously marks itself dirty many times between ticks and is
// sent once, so a client's traffic is bounded by time rather than by how noisy
// its panes are.
func (c *clientConn) pumpEvents(sub *Subscription, done <-chan struct{}) {
	dirty := make(map[session.PaneID]bool)
	tick := time.NewTicker(frameInterval)
	defer tick.Stop()

	for {
		select {
		case <-done:
			return

		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if ev.Kind == EventPaneOutput {
				// Only panes this client asked for. A client showing one tab
				// has no use for the others, and sending them would scale its
				// traffic with the size of the whole session.
				if c.isSubscribed(ev.Pane) {
					dirty[ev.Pane] = true
				}
				continue
			}
			if err := c.forward(ev); err != nil {
				return
			}

		case <-tick.C:
			for id := range dirty {
				delete(dirty, id)
				if !c.isSubscribed(id) {
					continue
				}
				if err := c.sendScreen(id); err != nil {
					return
				}
			}
		}
	}
}

// sendScreen sends a pane's current screen.
//
// The whole screen, not the bytes that produced it. A client that falls behind
// can miss any number of these and still be correct once the next arrives,
// because a screen describes a state rather than a change. Raw bytes would be
// cheaper on the wire and unrecoverable if one were ever dropped, which is the
// trade that decides it: a client can stall, and terminal output cannot be
// resynchronised after a gap.
func (c *clientConn) sendScreen(id session.PaneID) error {
	ansi, err := c.srv.RenderedScreen(id)
	if err != nil {
		return nil // the pane went away; not this connection's problem
	}
	return c.conn.WritePaneBytes(proto.FrameOutput, uint64(id), ansi)
}

// forward sends one non-output event.
func (c *clientConn) forward(ev Event) error {
	out := proto.Event{Pane: uint64(ev.Pane)}
	switch ev.Kind {
	case EventPaneOpened:
		out.Kind = proto.EventPaneOpened
	case EventPaneState:
		out.Kind = proto.EventPaneState
		out.State = ev.State.String()
		out.Rule = ev.Rule
	case EventPaneExited:
		out.Kind = proto.EventPaneExited
		out.Err = ev.Err
	case EventPaneClosed:
		out.Kind = proto.EventPaneClosed
	case EventPaneClipboard:
		out.Kind = proto.EventPaneClipboard
		out.Data = ev.Data
	case EventSessionChanged:
		out.Kind = proto.EventSessionChanged
	default:
		// An event kind this build does not map is dropped rather than sent
		// half-formed, so a client never sees a message it cannot interpret.
		return nil
	}
	return c.conn.WriteJSON(proto.FrameEvent, out)
}

func (c *clientConn) isSubscribed(id session.PaneID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscribed[id]
}

// serveRequests reads from the client until the connection ends.
func (c *clientConn) serveRequests() {
	for {
		frame, err := c.conn.ReadFrame()
		if err != nil {
			return // including io.EOF: the client hung up
		}

		switch frame.Type {
		case proto.FrameRequest:
			if err := c.handleRequest(frame.Payload); err != nil {
				return
			}
		case proto.FrameInput:
			pane, data, err := proto.DecodePaneBytes(frame.Payload)
			if err != nil {
				continue // a malformed input frame is dropped, not fatal
			}
			_ = c.srv.Write(session.PaneID(pane), data)
		default:
			// An unknown frame type is ignored, so a newer client can send
			// something this build has never heard of without being cut off.
			continue
		}
	}
}

func (c *clientConn) handleRequest(payload []byte) error {
	var req proto.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		// A request that will not parse has no id to answer, so there is
		// nothing to reply to and nothing to be gained by staying connected.
		return err
	}

	result, err := c.dispatch(req)
	if req.ID == 0 {
		if c.shutdown {
			go func() { _ = c.srv.Close() }()
		}
		c.commitHandoff()
		return nil // fire and forget
	}

	resp := proto.Response{ID: req.ID}
	if err != nil {
		resp.Error = err.Error()
	} else if result != nil {
		encoded, encErr := json.Marshal(result)
		if encErr != nil {
			resp.Error = encErr.Error()
		} else {
			resp.Result = encoded
		}
	}
	writeErr := c.conn.WriteJSON(proto.FrameResponse, resp)
	if c.shutdown {
		// The reply is on the wire now, so the client can tell success from a
		// crash even though its connection is about to be closed.
		go func() { _ = c.srv.Close() }()
	}
	c.commitHandoff()
	return writeErr
}

// commitHandoff lets go of the panes once a handoff this connection asked for
// has been answered.
func (c *clientConn) commitHandoff() {
	if h := c.handoff; h != nil {
		c.handoff = nil
		go func() { _ = c.srv.CommitHandoff(h) }()
	}
}

// dispatch runs one method. An unknown method is an error, never a
// disconnect: a missing feature should disable one action, not the session.
func (c *clientConn) dispatch(req proto.Request) (any, error) {
	switch req.Method {
	case proto.MethodHello:
		var p proto.HelloParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Version != proto.Version {
			return nil, fmt.Errorf("protocol version %d, this server speaks %d", p.Version, proto.Version)
		}
		methods := Methods
		if len(c.srv.cfg.Advertise) > 0 {
			methods = c.srv.cfg.Advertise
		}
		return proto.HelloResult{
			Version:  proto.Version,
			Server:   "tend",
			Build:    c.srv.cfg.Build,
			Methods:  methods,
			Features: c.srv.features(),
			Handoff:  c.srv.cfg.Replace != nil,
		}, nil

	case proto.MethodSessionSnapshot:
		return c.srv.snapshot(), nil

	case proto.MethodWorkspaceNew:
		var p proto.WorkspaceNewParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := c.srv.NewWorkspaceIn(p.Name, p.Dir)
		if err != nil {
			return nil, err
		}
		return proto.WorkspaceNewResult{Workspace: uint64(id)}, nil

	case proto.MethodTabNew:
		var p proto.TabNewParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		tab, pane, err := c.srv.NewTab(session.WorkspaceID(p.Workspace), p.Name, paneSpec(p.Pane))
		if err != nil {
			return nil, err
		}
		return proto.TabNewResult{Tab: uint64(tab), Pane: uint64(pane)}, nil

	case proto.MethodTabClose:
		var p proto.TabCloseParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.CloseTab(session.TabID(p.Tab))

	case proto.MethodWorkspaceClose:
		var p proto.WorkspaceCloseParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.CloseWorkspace(session.WorkspaceID(p.Workspace))

	case proto.MethodWorkspaceGroup:
		var p proto.WorkspaceGroupParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.GroupWorkspace(session.WorkspaceID(p.Workspace), p.Group)

	case proto.MethodTabRename:
		var p proto.TabRenameParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.RenameTab(session.TabID(p.Tab), p.Name)

	case proto.MethodWorkspaceRename:
		var p proto.WorkspaceRenameParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.RenameWorkspace(session.WorkspaceID(p.Workspace), p.Name)

	case proto.MethodPaneSplit:
		var p proto.PaneSplitParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		dir, err := parseDirection(p.Direction)
		if err != nil {
			return nil, err
		}
		pane, err := c.srv.SplitPane(session.PaneID(p.Target), dir, paneSpec(p.Pane))
		if err != nil {
			return nil, err
		}
		return proto.PaneSplitResult{Pane: uint64(pane)}, nil

	case proto.MethodPaneClose:
		var p proto.PaneCloseParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.ClosePane(session.PaneID(p.Pane))

	case proto.MethodPaneResize:
		var p proto.PaneResizeParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.Resize(session.PaneID(p.Pane), pty.Size{
			Cols: uint16(p.Cols),
			Rows: uint16(p.Rows),
		})

	case proto.MethodPaneSubscribe:
		var p proto.PaneSubscribeParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		c.subscribe(p.Panes)
		return nil, nil

	case proto.MethodPaneScreen:
		var p proto.PaneScreenParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.paneScreen(session.PaneID(p.Pane), p.Offset)

	case proto.MethodPaneText:
		var p proto.PaneTextParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.PaneText(session.PaneID(p.Pane), p)

	case proto.MethodPaneAdjust:
		var p proto.PaneAdjustParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		side, err := parseSide(p.Side)
		if err != nil {
			return nil, err
		}
		return nil, c.srv.AdjustSplit(session.PaneID(p.Target), side, p.Cells,
			session.Rect{W: p.Cols, H: p.Rows})

	case proto.MethodServerReloadConfig:
		return c.srv.ReloadFromFile(), nil

	case proto.MethodPaneRename:
		var p proto.PaneRenameParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.RenamePane(session.PaneID(p.Pane), p.Name)

	case proto.MethodPaneFocus:
		var p proto.PaneFocusParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		c.srv.FocusPane(session.PaneID(p.Pane), session.PaneID(p.Lost))
		return nil, nil

	case proto.MethodPaneGraphics:
		var p proto.PaneScreenParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.PaneGraphics(session.PaneID(p.Pane))

	case proto.MethodPaneSwap:
		var p proto.PaneSwapParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Target != 0 {
			return proto.PaneSwapResult{Other: p.Target},
				c.srv.SwapPanes(session.PaneID(p.Pane), session.PaneID(p.Target))
		}
		side, err := parseSide(p.Side)
		if err != nil {
			return nil, err
		}
		other, err := c.srv.SwapPaneToward(session.PaneID(p.Pane), side, session.Rect{W: p.Cols, H: p.Rows})
		return proto.PaneSwapResult{Other: uint64(other)}, err

	case proto.MethodTabMove:
		var p proto.MoveParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.MoveTab(session.TabID(p.ID), p.Index, p.Delta)

	case proto.MethodWorkspaceMove:
		var p proto.MoveParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, c.srv.MoveWorkspace(session.WorkspaceID(p.ID), p.Index, p.Delta)

	case proto.MethodTabLayout:
		var p proto.TabLayoutParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.tabLayout(session.TabID(p.Tab), p.Cols, p.Rows)

	case proto.MethodServerShutdown:
		c.shutdown = true
		return nil, nil

	case proto.MethodPaneCopyMotion:
		var p proto.PaneCopyMotionParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.CopyMotion(session.PaneID(p.Pane), p.From, p.Motion)

	case proto.MethodPaneCopySearch:
		var p proto.PaneCopySearchParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.srv.CopySearch(session.PaneID(p.Pane), p.From, p.Query, p.Direction)

	case proto.MethodServerHandoff:
		// Carried out before answering, so the answer can say whether it
		// worked: by the time the client reads "ok" the replacement is already
		// accepting on the socket. Letting go of the panes waits for the reply
		// to be written, like a shutdown does.
		h, err := c.srv.Replace()
		if err != nil {
			return nil, err
		}
		c.handoff = h
		return nil, nil
	}

	return nil, fmt.Errorf("%s: %w", req.Method, proto.ErrUnknownMethod)
}

// subscribe replaces the set of panes this client receives output for.
func (c *clientConn) subscribe(panes []uint64) {
	next := make(map[session.PaneID]bool, len(panes))
	for _, id := range panes {
		next[session.PaneID(id)] = true
	}
	c.mu.Lock()
	c.subscribed = next
	c.mu.Unlock()

	// Send what is already on screen, or a client that subscribes to an idle
	// pane sees nothing until the pane happens to produce output again.
	for id := range next {
		_ = c.sendScreen(id)
	}
}

func decodeParams(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("bad params: %w", err)
	}
	return nil
}

func parseDirection(s string) (session.Direction, error) {
	switch s {
	case "columns", "":
		return session.Columns, nil
	case "rows":
		return session.Rows, nil
	}
	return 0, fmt.Errorf("unknown direction %q; use \"columns\" or \"rows\"", s)
}

func parseSide(s string) (session.Side, error) {
	switch s {
	case "left":
		return session.Left, nil
	case "right":
		return session.Right, nil
	case "up":
		return session.Up, nil
	case "down":
		return session.Down, nil
	}
	return 0, fmt.Errorf("unknown side %q; use left, right, up or down", s)
}

func paneSpec(p proto.PaneSpec) PaneSpec {
	return PaneSpec{
		Command: p.Command,
		Dir:     p.Dir,
		Env:     p.Env,
		Title:   p.Title,
		Agent:   p.Agent,
		Size:    pty.Size{Cols: uint16(p.Cols), Rows: uint16(p.Rows)},
	}
}

// --- server helpers used by connections ------------------------------------

// snapshot describes the whole session.
func (s *Server) snapshot() proto.SessionSnapshot {
	var snap proto.SessionSnapshot

	s.mu.Lock()
	sess := s.session
	if active := sess.ActiveWorkspace(); active != nil {
		snap.ActiveWorkspace = uint64(active.ID)
	}
	for _, w := range sess.Workspaces() {
		info := proto.WorkspaceInfo{
			ID:    uint64(w.ID),
			Name:  w.Name,
			Dir:   w.Dir,
			Group: w.Group,
		}
		if at := w.ActiveTab(); at != nil {
			info.ActiveTab = uint64(at.ID)
		}
		for _, tab := range w.Tabs() {
			ti := proto.TabInfo{
				ID:         uint64(tab.ID),
				Name:       tab.Name,
				ActivePane: uint64(tab.ActivePane()),
			}
			for _, id := range tab.Panes() {
				ti.Panes = append(ti.Panes, uint64(id))
				if p, ok := tab.Pane(id); ok {
					snap.Panes = append(snap.Panes, proto.PaneInfo{
						ID:      uint64(p.ID),
						Title:   p.Title,
						Named:   p.Named,
						Agent:   p.Agent,
						State:   p.State.String(),
						Command: p.Command,
						Dir:     p.Dir,
					})
				}
			}
			info.Tabs = append(info.Tabs, ti)
		}
		snap.Workspaces = append(snap.Workspaces, info)
	}
	runtimes := make(map[session.PaneID]*paneRuntime, len(s.runtimes))
	for id, rt := range s.runtimes {
		runtimes[id] = rt
	}
	s.mu.Unlock()

	// Branches are resolved after the session lock is released: reading one
	// touches the filesystem, and nothing that does belongs under the lock
	// every pane operation needs.
	for i := range snap.Workspaces {
		dir := snap.Workspaces[i].Dir
		snap.Workspaces[i].Branch = s.branches.lookup(dir)
		t := s.branches.tracking(dir)
		snap.Workspaces[i].Ahead, snap.Workspaces[i].Behind = t.Ahead, t.Behind
	}

	// Runtime facts come from the runtimes, under their own locks, once the
	// session lock is released.
	for i := range snap.Panes {
		rt, ok := runtimes[session.PaneID(snap.Panes[i].ID)]
		if !ok {
			continue
		}
		st := rt.status()
		snap.Panes[i].Running = st.Running
		snap.Panes[i].Pid = st.Pid
		snap.Panes[i].ExitErr = st.ExitErr
		snap.Panes[i].Rule = st.Rule
		snap.Panes[i].Mouse = st.Mouse
		snap.Panes[i].MouseDrag = st.MouseDrag
		snap.Panes[i].MouseMotion = st.MouseMotion
		snap.Panes[i].MouseSGR = st.MouseSGR
		snap.Panes[i].Graphics = st.Graphics
		if st.Title != "" && !snap.Panes[i].Named {
			// A pane the user named keeps that name. The program's terminal
			// title is what a pane is called when nobody has said otherwise.
			snap.Panes[i].Title = st.Title
		}
	}
	return snap
}

// tabLayout computes where a tab's panes go at the size the client asked for.
func (s *Server) tabLayout(id session.TabID, cols, rows int) (proto.TabLayoutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tab, ok := s.session.Tab(id)
	if !ok {
		return proto.TabLayoutResult{}, fmt.Errorf("%w: %d", session.ErrNoSuchTab, id)
	}
	if cols < 1 || rows < 1 {
		return proto.TabLayoutResult{}, fmt.Errorf("layout size %dx%d is empty", cols, rows)
	}

	var out proto.TabLayoutResult
	for _, r := range tab.Layout(session.Rect{W: cols, H: rows}) {
		out.Panes = append(out.Panes, proto.PaneRect{
			Pane: uint64(r.Pane),
			X:    r.Rect.X,
			Y:    r.Rect.Y,
			Cols: r.Rect.W,
			Rows: r.Rect.H,
		})
	}
	return out, nil
}

func (s *Server) paneScreen(id session.PaneID, offset int) (proto.PaneScreenResult, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return proto.PaneScreenResult{}, err
	}
	var out proto.PaneScreenResult
	rt.withScreen(func(scr *vt.Screen) {
		out.Cols, out.Rows = scr.Size()
		out.Title = scr.Title()
	})
	ansi, actual, history := rt.scrolledScreen(offset)
	out.ANSI = string(ansi)
	out.Offset = actual
	out.History = history
	out.Text = rt.screenText()
	return out, nil
}

// PaneText returns the text in a region of a pane.
func (s *Server) PaneText(id session.PaneID, p proto.PaneTextParams) (proto.PaneTextResult, error) {
	s.mu.Lock()
	rt, ok := s.runtimes[id]
	s.mu.Unlock()
	if !ok {
		return proto.PaneTextResult{}, fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}

	var out proto.PaneTextResult
	rt.withScreen(func(screen *vt.Screen) {
		out.Text = paneText(screen, p)
	})
	return out, nil
}

// paneText cuts a region out of a terminal.
//
// A viewport row is turned into a position in everything the pane has — the
// history first, then the live screen — so a row above the view is three lines
// into the history and the arithmetic is the same either side of that line.
func paneText(screen *vt.Screen, p proto.PaneTextParams) string {
	// The visible rows come from whichever grid is showing. A full-screen
	// program is on the alternate one, and reading the main grid there
	// returns the shell it was started from — text the user is not looking
	// at and did not select.
	grid := screen.Grid()
	main := screen.MainGrid()

	// Only the main screen has a history. The alternate screen not having one
	// is what it is for: a program that takes the whole terminal is expected
	// to put it back as it found it.
	history := 0
	if grid == main {
		history = main.HistoryLen()
	}
	total := history + grid.Rows()
	if total == 0 {
		return ""
	}

	fromRow, fromCol, toRow, toCol := ordered(p)
	top := history - max(p.Scroll, 0)
	first := min(max(top+fromRow, 0), total-1)
	last := min(max(top+toRow, 0), total-1)

	var out []byte
	for at := first; at <= last; at++ {
		row := grid.Line(at - history)
		if at < history {
			row = main.HistoryLine(at)
		}
		if at > first {
			out = append(out, '\n')
		}
		if row == nil {
			continue
		}

		start, end := fromCol, toCol
		if !p.Block {
			// A run takes the end of the first line, all of the middle ones
			// and the start of the last, which is what dragging over prose is
			// asking for.
			start, end = 0, row.Len()-1
			if at == first {
				start = fromCol
			}
			if at == last {
				end = toCol
			}
		}
		out = append(out, trimRight(cellsOf(row, start, end))...)
	}
	return string(out)
}

// ordered puts the corners of a region in reading order.
//
// A rectangle has corners and a run has a beginning and an end, so which of
// the two ends came first matters for one and not the other.
func ordered(p proto.PaneTextParams) (fromRow, fromCol, toRow, toCol int) {
	if p.Block {
		return min(p.FromRow, p.ToRow), min(p.FromCol, p.ToCol),
			max(p.FromRow, p.ToRow), max(p.FromCol, p.ToCol)
	}
	if p.FromRow < p.ToRow || (p.FromRow == p.ToRow && p.FromCol <= p.ToCol) {
		return p.FromRow, p.FromCol, p.ToRow, p.ToCol
	}
	return p.ToRow, p.ToCol, p.FromRow, p.FromCol
}

// cellsOf reads a row's runes between two columns, counting cells rather than
// runes: a wide character fills two columns and its second half holds no rune
// of its own.
func cellsOf(row *vt.Row, from, to int) []byte {
	var out []byte
	for x := max(from, 0); x <= to && x < row.Len(); x++ {
		cell := row.Cell(x)
		if cell.Width == 0 {
			continue
		}
		r := cell.R
		if r == 0 {
			r = ' '
		}
		out = append(out, []byte(string(r))...)
	}
	return out
}

// trimRight drops the blanks a terminal pads its rows with. Pasting them turns
// one line of code into one line and seventy spaces.
func trimRight(line []byte) []byte {
	at := len(line)
	for at > 0 && line[at-1] == ' ' {
		at--
	}
	return line[:at]
}
