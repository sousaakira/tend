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
	// MethodServerHandoff replaces the server with a new process running the
	// binary now on disk, keeping every pane's program. The reply comes once
	// the replacement holds the panes; the connection drops right after, and
	// reconnecting reaches the new server on the same socket.
	MethodServerHandoff = "server.handoff"
	// MethodPaneCopyMotion and MethodPaneCopySearch are copy mode's questions
	// to the server: where a motion lands, and where the next match is. The
	// cursor lives in the client; the text it moves through lives here.
	MethodPaneCopyMotion = "pane.copy_motion"
	MethodPaneCopySearch = "pane.copy_search"
	// MethodPaneSwap, MethodTabMove and MethodWorkspaceMove rearrange without
	// making anything again: the programs keep running wherever they end up.
	// MethodServerReloadConfig makes the server re-read the settings file.
	MethodServerReloadConfig = "server.reload_config"
	// MethodPaneFocus says which pane the client is looking at, so programs
	// that asked for focus events are told.
	MethodPaneFocus = "pane.focus"
	// MethodPaneRename gives a pane a name that its program cannot overwrite.
	MethodPaneRename = "pane.rename"
	// MethodPaneEditScrollback opens a pane's history in an editor, in a pane
	// of its own.
	MethodPaneEditScrollback = "pane.edit_scrollback"
	// MethodPaneGraphics fetches the images a pane holds and where they go.
	MethodPaneGraphics  = "pane.graphics"
	MethodPaneSwap      = "pane.swap"
	MethodTabMove       = "tab.move"
	MethodWorkspaceMove = "workspace.move"
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
// KnownMethods is every method this build of the protocol has a name for.
//
// It exists so the two halves can work out which of them is behind. Builds are
// git descriptions with no ordering, so comparing them says only that they
// differ; what one side can do and the other has never heard of is a fact with
// a direction in it.
var KnownMethods = []string{
	MethodHello,
	MethodSessionSnapshot,
	MethodWorkspaceNew,
	MethodWorkspaceClose,
	MethodWorkspaceGroup,
	MethodWorkspaceRename,
	MethodTabNew,
	MethodTabClose,
	MethodTabRename,
	MethodTabLayout,
	MethodPaneSplit,
	MethodPaneClose,
	MethodPaneResize,
	MethodPaneSubscribe,
	MethodPaneScreen,
	MethodPaneText,
	MethodPaneAdjust,
	MethodServerShutdown,
	MethodServerHandoff,
	MethodPaneCopyMotion,
	MethodPaneCopySearch,
	MethodServerReloadConfig,
	MethodPaneFocus,
	MethodPaneRename,
	MethodPaneEditScrollback,
	MethodPaneGraphics,
	MethodPaneSwap,
	MethodTabMove,
	MethodWorkspaceMove,
}

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
	// Features are things a server does that are not methods: events it
	// sends, fields it fills in. A change of that kind is invisible in the
	// method list, and it is how a client ended up forwarding the mouse to a
	// server that did not say which encoding the program wanted, and waiting
	// for a clipboard event that server had never heard of.
	Features []string `json:"features,omitempty"`
	// Handoff reports that this server can be replaced without ending its
	// programs. Knowing the method is not the same thing: a server run by
	// something other than `tend serve` has the method and no way to start a
	// replacement, and a client that offered "everything keeps running" on the
	// strength of the method alone would be promising what it cannot deliver.
	Handoff bool `json:"handoff,omitempty"`
}

// Features a server of this build provides.
const (
	// FeaturePaneClipboard: a pane's OSC 52 write is passed on as an event.
	FeaturePaneClipboard = "pane-clipboard"
	// FeatureMouseDetail: the snapshot says how much mouse a pane asked for
	// and in which encoding.
	FeatureMouseDetail = "mouse-detail"
	// FeatureSessionChanged: the server says when panes are swapped, and tabs
	// or spaces moved, renamed or regrouped.
	FeatureSessionChanged = "session-changed"
	// FeatureGraphics: the server keeps the images a pane's program sent and
	// hands them over, so a client can draw them on its own terminal.
	FeatureGraphics = "graphics"
	// FeatureLifecycle: the server reports what is focused and what was
	// created, which plugins hook on.
	FeatureLifecycle = "lifecycle"
)

// KnownFeatures is every feature this build knows of, for the same reason
// KnownMethods exists.
var KnownFeatures = []string{
	FeaturePaneClipboard, FeatureMouseDetail, FeatureSessionChanged, FeatureGraphics,
	FeatureLifecycle,
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
	Mouse bool `json:"mouse,omitempty"`
	// MouseDrag and MouseMotion say how much the program subscribed to, and
	// MouseSGR how it wants reports written.
	MouseDrag   bool `json:"mouse_drag,omitempty"`
	MouseMotion bool `json:"mouse_motion,omitempty"`
	MouseSGR    bool `json:"mouse_sgr,omitempty"`
	// Graphics changes whenever a pane's images or their placements do, so a
	// client can tell whether to ask for them again.
	Graphics uint64 `json:"graphics,omitempty"`
	// Named marks a title the user gave the pane, which its program's own
	// title does not replace.
	Named bool `json:"named,omitempty"`
	// Display is what to call the agent, and Tokens are the values a hook
	// asked to have shown beside it — the model, what is left of the
	// context. StateLabels rename a state for this pane.
	Display     string            `json:"display,omitempty"`
	Tokens      []AgentToken      `json:"tokens,omitempty"`
	StateLabels map[string]string `json:"state_labels,omitempty"`
	ExitErr     string            `json:"exit_error,omitempty"`

	Command []string `json:"command,omitempty"`
	Dir     string   `json:"dir,omitempty"`
}

// AgentToken is one value to show beside an agent.
type AgentToken struct {
	Key   string `json:"key"`
	Value string `json:"value"`
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

// CopyPoint is a cell by absolute row: row 0 is the oldest line the pane
// keeps. Copy mode addresses the whole history this way, because a viewport
// row means a different line every time the view moves.
type CopyPoint struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// PaneCopyMotionParams asks where a motion from a point lands.
type PaneCopyMotionParams struct {
	Pane   uint64    `json:"pane"`
	From   CopyPoint `json:"from"`
	Motion string    `json:"motion"`
}

// PaneCopySearchParams asks for the nearest match of a query.
type PaneCopySearchParams struct {
	Pane      uint64    `json:"pane"`
	From      CopyPoint `json:"from"`
	Query     string    `json:"query"`
	Direction string    `json:"direction"`
}

// PaneCopyResult is where the cursor goes, and how much history there is now,
// so the client can turn the absolute row into a view.
type PaneCopyResult struct {
	To      CopyPoint `json:"to"`
	End     CopyPoint `json:"end,omitempty"`
	Found   bool      `json:"found,omitempty"`
	Total   int       `json:"total,omitempty"`
	History int       `json:"history"`
	Rows    int       `json:"rows"`
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

// PaneRenameParams names a pane.
type PaneRenameParams struct {
	Pane uint64 `json:"pane"`
	Name string `json:"name"`
}

// PaneFocusParams says which pane has the focus now and which had it.
type PaneFocusParams struct {
	Pane uint64 `json:"pane"`
	Lost uint64 `json:"lost,omitempty"`
}

// GraphicsImage is one image a pane holds, as bytes to pass on.
type GraphicsImage struct {
	ID     uint32 `json:"id"`
	Format uint8  `json:"format"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	// Data is the image exactly as the program sent it, base64 in JSON.
	Data []byte `json:"data"`
}

// GraphicsPlacement is where an image goes, in the pane's own rows counted
// from the oldest line it keeps.
type GraphicsPlacement struct {
	ImageID uint32 `json:"image"`
	ID      uint32 `json:"id,omitempty"`
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Z       int    `json:"z,omitempty"`
}

// PaneGraphicsResult is a pane's images and placements.
type PaneGraphicsResult struct {
	Revision   uint64              `json:"revision"`
	History    int                 `json:"history"`
	Images     []GraphicsImage     `json:"images,omitempty"`
	Placements []GraphicsPlacement `json:"placements,omitempty"`
}

// ReloadResult says what a reload changed, or what was wrong with the file.
type ReloadResult struct {
	Path    string   `json:"path"`
	Changed []string `json:"changed,omitempty"`
	Err     string   `json:"error,omitempty"`
}

// PaneSwapParams exchanges a pane with another, named or found on a side of it
// as laid out at Cols by Rows.
type PaneSwapParams struct {
	Pane   uint64 `json:"pane"`
	Target uint64 `json:"target,omitempty"`
	Side   string `json:"side,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

// PaneSwapResult names the pane it traded places with.
type PaneSwapResult struct {
	Other uint64 `json:"other"`
}

// MoveParams puts a tab or a space at an index in its row. Delta, when set,
// moves it that many places instead, which is what a key binding asks for.
type MoveParams struct {
	ID    uint64 `json:"id"`
	Index int    `json:"index"`
	Delta int    `json:"delta,omitempty"`
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
	// EventPaneClipboard carries text a pane's program asked to have copied.
	EventPaneClipboard = "pane-clipboard"
	// EventSessionChanged says the session's shape changed with no pane
	// opened or closed. It carries nothing: the client reads the session.
	EventSessionChanged = "session-changed"
	// EventNotify carries something to tell the user: a title and a body.
	EventNotify = "notify"
	// The events about what is focused and what was created. A client sends
	// focus and every client hears it, which is how a second one follows
	// along; a plugin hears them under herdr's names.
	EventPaneFocused      = "pane-focused"
	EventTabFocused       = "tab-focused"
	EventWorkspaceFocused = "workspace-focused"
	EventTabCreated       = "tab-created"
	EventWorkspaceCreated = "workspace-created"
)

// Event is something that happened, sent unsolicited.
type Event struct {
	Kind string `json:"kind"`
	Pane uint64 `json:"pane,omitempty"`

	State string `json:"state,omitempty"`
	Rule  string `json:"rule,omitempty"`
	Err   string `json:"error,omitempty"`
	// Data is the text of a clipboard event. JSON carries it as base64.
	Data []byte `json:"data,omitempty"`
	// Title and Body are set on a notify event.
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Tab and Workspace are set on the events about them.
	Tab       uint64 `json:"tab,omitempty"`
	Workspace uint64 `json:"workspace,omitempty"`
}

// Compare reports which side of a connection knows things the other does not,
// given the methods a server advertises.
//
// Both can be true at once: two builds that each added something the other
// lacks have diverged rather than one being behind, and saying "you are old"
// to either of them would be wrong.
func Compare(advertised []string) (serverAhead, clientAhead bool) {
	return compare(advertised, KnownMethods, MethodHello)
}

// CompareFeatures is Compare for the things that are not methods.
func CompareFeatures(advertised []string) (serverAhead, clientAhead bool) {
	return compare(advertised, KnownFeatures, "")
}

func compare(advertised, mine []string, skip string) (serverAhead, clientAhead bool) {
	has := make(map[string]bool, len(advertised))
	for _, m := range advertised {
		has[m] = true
	}
	known := make(map[string]bool, len(mine))
	for _, m := range mine {
		known[m] = true
	}

	for _, m := range advertised {
		if !known[m] {
			serverAhead = true
		}
	}
	for _, m := range mine {
		if m != skip && !has[m] {
			clientAhead = true
		}
	}
	return serverAhead, clientAhead
}
