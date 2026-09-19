package proto

import "encoding/json"

// Method names are permanent. Renaming one is a protocol break; adding one is
// not, because an unknown method is answered with an error rather than a
// disconnect, so an older server disables one action instead of refusing the
// client.
const (
	MethodHello = "hello"

	MethodSessionSnapshot = "session.snapshot"

	MethodWorkspaceNew = "workspace.new"

	MethodTabNew   = "tab.new"
	MethodTabClose = "tab.close"

	MethodPaneSplit     = "pane.split"
	MethodPaneClose     = "pane.close"
	MethodPaneResize    = "pane.resize"
	MethodPaneSubscribe = "pane.subscribe"
	MethodPaneScreen    = "pane.screen"

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
type HelloResult struct {
	Version int      `json:"version"`
	Server  string   `json:"server,omitempty"`
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

	Running bool   `json:"running"`
	Pid     int    `json:"pid,omitempty"`
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
	ID        uint64    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Tabs      []TabInfo `json:"tabs"`
	ActiveTab uint64    `json:"active_tab,omitempty"`
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

// PaneScreenParams asks for a pane's current screen.
type PaneScreenParams struct {
	Pane uint64 `json:"pane"`
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
