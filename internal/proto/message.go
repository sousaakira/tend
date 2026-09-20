package proto

import (
	"encoding/json"
	"errors"
)

// Method names are permanent. Renaming one is a protocol break; adding one is
// not, because an unknown method is answered with an error rather than a
// disconnect, so an older server disables one action instead of refusing the
// client.
const (
	MethodHello = "hello"

	MethodSessionSnapshot = "session.snapshot"

	MethodWorkspaceNew = "workspace.new"

	MethodTabNew    = "tab.new"
	MethodTabClose  = "tab.close"
	MethodTabRename = "tab.rename"

	MethodWorkspaceClose  = "workspace.close"
	MethodWorkspaceGroup  = "workspace.group"
	MethodWorkspaceRename = "workspace.rename"

	MethodPaneSplit     = "pane.split"
	MethodPaneClose     = "pane.close"
	MethodPaneResize    = "pane.resize"
	MethodPaneSubscribe = "pane.subscribe"
	MethodPaneScreen    = "pane.screen"
	MethodPaneText      = "pane.text"

	MethodPaneAdjust = "pane.adjust"

	MethodTabLayout = "tab.layout"

	MethodServerShutdown = "server.shutdown"
)

// Request is a call from a client.
type Request struct {
	// ID pairs a response with its request. Zero means the client wants no
	// response, which is how it sends fire-and-forget calls.
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a Request. Exactly one of Error and Result is meaningful.
type Response struct {
	ID     uint64          `json:"id"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// --- handshake -------------------------------------------------------------

// HelloParams is what a client announces.
type HelloParams struct {
	Version int    `json:"version"`
	Client  string `json:"client,omitempty"`
}

// HelloResult is what the server answers.
//
// Methods lists what this server actually implements, so a client can disable
// an action it cannot perform rather than discovering the gap when a user
// tries it.
// ErrUnknownMethod is what a server answers when it has never heard of a
// method. A client recognises it to tell "this cannot be done" apart from
// "the server on the other end is older than you are", which are the same
// failure to anyone reading a raw protocol error.
var ErrUnknownMethod = errors.New("unknown method")

type HelloResult struct {
	Version int    `json:"version"`
	Server  string `json:"server,omitempty"`
	// Build is the server binary's version. The protocol version says the two
	// sides can talk; this says whether they are the same age, which is what
	// a client needs to explain a method the server has never heard of.
	Build   string   `json:"build,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

// --- session ---------------------------------------------------------------

// PaneInfo describes one pane.
type PaneInfo struct {
	ID    uint64 `json:"id"`
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	State string `json:"state"`
	Rule  string `json:"rule,omitempty"`

	Running bool `json:"running"`
	Pid     int  `json:"pid,omitempty"`
	// Mouse is the mouse reporting the pane's own program asked for, so a
	// client knows whether a click belongs to the application or to tend.
	Mouse   bool   `json:"mouse,omitempty"`
	ExitErr string `json:"exit_error,omitempty"`

	Command []string `json:"command,omitempty"`
	Dir     string   `json:"dir,omitempty"`
}

// TabInfo describes a tab and the panes in it.
type TabInfo struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name,omitempty"`
	Panes      []uint64 `json:"panes"`
	ActivePane uint64   `json:"active_pane"`
}

// WorkspaceInfo describes a workspace.
type WorkspaceInfo struct {
	ID   uint64 `json:"id"`
	Name string `json:"name,omitempty"`
	// Dir is what the workspace is about, and Branch the git branch checked
	// out there. Branch is computed by the server because only it can see the
	// directory: a client may be on another machine entirely.
	Dir    string `json:"dir,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Ahead and Behind are how far the checkout has drifted from the branch
	// it follows. Zero for both means in step, or nothing to be in step with.
	Ahead  int `json:"ahead,omitempty"`
	Behind int `json:"behind,omitempty"`
	// Group is what the workspace is kept with. Empty means it stands alone.
	Group     string    `json:"group,omitempty"`
	Tabs      []TabInfo `json:"tabs"`
	ActiveTab uint64    `json:"active_tab,omitempty"`
}

// PaneTextParams asks for the text in a region of a pane.
//
// The rows are counted from the top of the view at Scroll and may fall outside
// it: a selection dragged past the edge covers text the client has scrolled
// away from, and the point of asking is that the server still has it.
//
// The region is cut where the cells are rather than by the caller, because a
// column is a property of the terminal: a wide character fills two of them and
// a combining mark none, so counting runes in a line of text gets a different
// answer than counting cells on a screen.
type PaneTextParams struct {
	Pane   uint64 `json:"pane"`
	Scroll int    `json:"scroll,omitempty"`
	// FromRow and ToRow are inclusive viewport rows; FromCol and ToCol are
	// the columns where the drag started and ended.
	FromRow int `json:"from_row"`
	FromCol int `json:"from_col"`
	ToRow   int `json:"to_row"`
	ToCol   int `json:"to_col"`
	// Block takes a rectangle instead of a run of text.
	Block bool `json:"block,omitempty"`
}

// PaneTextResult carries what the region holds.
type PaneTextResult struct {
	Text string `json:"text"`
}

// SessionSnapshot is the whole session as a client sees it.
type SessionSnapshot struct {
	Workspaces      []WorkspaceInfo `json:"workspaces"`
	ActiveWorkspace uint64          `json:"active_workspace,omitempty"`
	Panes           []PaneInfo      `json:"panes"`
}

// --- pane operations -------------------------------------------------------

// PaneSpec describes a pane to start.
type PaneSpec struct {
	Command []string `json:"command"`
	Dir     string   `json:"dir,omitempty"`
	Env     []string `json:"env,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	Title   string   `json:"title,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
}

// WorkspaceNewParams creates a workspace.
type WorkspaceNewParams struct {
	Name string `json:"name,omitempty"`
	Dir  string `json:"dir,omitempty"`
}

// WorkspaceCloseParams closes a workspace and every pane in it.
type WorkspaceCloseParams struct {
	Workspace uint64 `json:"workspace"`
}

// WorkspaceGroupParams moves a workspace into a group, or out of one when the
// group is empty.
type WorkspaceGroupParams struct {
	Workspace uint64 `json:"workspace"`
	Group     string `json:"group,omitempty"`
}

// WorkspaceNewResult reports the new workspace.
type WorkspaceNewResult struct {
	Workspace uint64 `json:"workspace"`
}

// TabNewParams creates a tab with one pane.
type TabNewParams struct {
	Workspace uint64   `json:"workspace"`
	Name      string   `json:"name,omitempty"`
	Pane      PaneSpec `json:"pane"`
}

// TabNewResult reports the new tab and its first pane.
type TabNewResult struct {
	Tab  uint64 `json:"tab"`
	Pane uint64 `json:"pane"`
}

// TabCloseParams closes a tab and every pane in it.
type TabCloseParams struct {
	Tab uint64 `json:"tab"`
}

// TabRenameParams renames a tab.
type TabRenameParams struct {
	Tab  uint64 `json:"tab"`
	Name string `json:"name"`
}

// WorkspaceRenameParams renames a workspace.
type WorkspaceRenameParams struct {
	Workspace uint64 `json:"workspace"`
	Name      string `json:"name"`
}

// PaneSplitParams divides a pane.
type PaneSplitParams struct {
	Target uint64 `json:"target"`
	// Direction is "columns" to place the new pane beside the target, or
	// "rows" to place it below. The names describe the arrangement rather than
	// the divider, which is the one naming choice in a multiplexer that
	// reliably confuses people.
	Direction string   `json:"direction"`
	Pane      PaneSpec `json:"pane"`
}

// PaneSplitResult reports the new pane.
type PaneSplitResult struct {
	Pane uint64 `json:"pane"`
}

// PaneCloseParams closes one pane.
type PaneCloseParams struct {
	Pane uint64 `json:"pane"`
}

// PaneResizeParams changes a pane's terminal size.
type PaneResizeParams struct {
	Pane uint64 `json:"pane"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// PaneAdjustParams moves one edge of a pane, taking the space from the
// neighbour across it.
//
// Cells and the area are both given because a divider is stored as a
// proportion of its split while the user is asking in cells, and only the
// client knows how many cells the tab is being drawn in.
type PaneAdjustParams struct {
	Pane string `json:"-"`

	Target uint64 `json:"target"`
	// Side is "left", "right", "up" or "down": the edge of the pane to move.
	Side string `json:"side"`
	// Cells is how far to move it. Negative shrinks.
	Cells int `json:"cells"`
	Cols  int `json:"cols"`
	Rows  int `json:"rows"`
}

// PaneSubscribeParams chooses which panes stream their output to this client.
//
// Panes replaces the current set rather than adding to it, so a client that
// switches tabs sends one message instead of unsubscribing and resubscribing
// and briefly receiving both or neither.
type PaneSubscribeParams struct {
	Panes []uint64 `json:"panes"`
}

// PaneScreenParams asks for a pane's screen.
type PaneScreenParams struct {
	Pane uint64 `json:"pane"`
	// Offset is how many lines back through the scrollback to look. Zero is
	// the live screen.
	Offset int `json:"offset,omitempty"`
}

// PaneScreenResult carries a pane's screen.
//
// Text is what a human or a script reads; ANSI is what a terminal draws. Both
// are sent because they answer different questions, and a client that only
// wants to list panes should not have to parse escape sequences to do it.
type PaneScreenResult struct {
	Text  string `json:"text"`
	ANSI  string `json:"ansi,omitempty"`
	Title string `json:"title,omitempty"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	// History is how many lines are behind the top of the screen, so a client
	// knows how far back it can scroll without asking and being refused.
	History int `json:"history,omitempty"`
	// Offset is how far back this view actually is, which may be less than
	// was asked for.
	Offset int `json:"offset,omitempty"`
}

// TabLayoutParams asks where a tab's panes go at a given size.
//
// The size is the client's, not the server's: the layout tree stores
// proportions rather than cells, so two clients of different sizes get
// different rectangles from the same tab without either being wrong.
type TabLayoutParams struct {
	Tab  uint64 `json:"tab"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// PaneRect is where one pane sits, in cells.
type PaneRect struct {
	Pane uint64 `json:"pane"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// TabLayoutResult is the arrangement of a tab's panes.
type TabLayoutResult struct {
	Panes []PaneRect `json:"panes"`
}

// --- events ----------------------------------------------------------------

// Event kind names are permanent. A client that does not know one must ignore
// it rather than treat it as an error, so a newer server can report more
// without breaking an older client.
const (
	EventPaneOpened = "pane-opened"
	EventPaneState  = "pane-state"
	EventPaneExited = "pane-exited"
	EventPaneClosed = "pane-closed"
)

// Event is something that happened, sent unsolicited.
type Event struct {
	Kind string `json:"kind"`
	Pane uint64 `json:"pane,omitempty"`

	State string `json:"state,omitempty"`
	Rule  string `json:"rule,omitempty"`
	Err   string `json:"error,omitempty"`
}
