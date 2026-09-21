package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/sousaakira/tend/internal/client"
	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/ui"
	"github.com/sousaakira/tend/internal/vt"
)

// frameInterval is how often the screen is repainted when something changed.
// Faster than this is invisible; slower is felt while typing.
const frameInterval = 16 * time.Millisecond

// messageLinger is how long a status message stays before the pane list
// returns.
const messageLinger = 3 * time.Second

// reconnectWindow is how long to keep trying after the server goes away.
// Long enough to outlast a restart; short enough that a session which is truly
// gone does not leave the client sitting on a dead screen.
const reconnectWindow = 30 * time.Second

// resizeStep is how far one resize key press moves a divider. Small enough to
// aim with, large enough that adjusting a pane is not a drum solo.
const resizeStep = 3

// runAttach draws a session and forwards keys to it.
func runAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ExitOnError)
	name := sessionFlag(fs)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend attach [-s session] [-ssh user@host]\n\n"+
				"draws a session's panes and forwards the keyboard to the focused one.\n"+
				"ctrl+b is the prefix; ctrl+b ? lists the keys. ctrl+b d detaches,\n"+
				"leaving everything running.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The settings are read before anything else is checked. A file that will
	// not load is a problem wherever tend is being run from, and reporting
	// the terminal first would hide it from anyone who noticed by piping the
	// output somewhere.
	cfg, err := config.Load()
	if err != nil {
		// Running with settings the user did not write, silently, is worse
		// than refusing to start.
		return err
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("attach needs a terminal; use \"tend follow\" when output is redirected")
	}
	prefix, _ := cfg.PrefixKey()
	if prefix == 0 {
		prefix = ui.Disabled
	}

	t := &tui{
		config:   cfg,
		session:  *name,
		host:     remoteHost,
		theme:    ui.ThemeFrom(cfg.UI.Theme),
		painter:  vt.NewPainter(),
		screens:  make(map[uint64]*vt.Screen),
		sizes:    make(map[uint64]ui.Rect),
		wake:     make(chan struct{}, 1),
		input:    make(chan []byte, 64),
		lostConn: make(chan struct{}, 1),
		resync:   make(chan struct{}, 1),
		folded:   make(map[string]bool),
	}
	t.keys.PrefixKey = prefix
	t.sidebar = cfg.UI.Sidebar
	t.grouped = cfg.UI.Grouped
	return t.run()
}

// tui is one attached client.
//
// The server pushes from its own goroutine, so everything it delivers is
// recorded under this lock and the draw loop is only woken. Drawing from the
// delivering goroutine would put the screen at the mercy of how fast a pane
// produces output.
type tui struct {
	config  config.Config
	session string
	// host is the machine the session is on, or empty for this one. It is
	// kept so a reconnect goes back to the same place it lost.
	host    string
	theme   ui.Theme
	client  *client.Client
	painter *vt.Painter

	mu        sync.Mutex
	snap      proto.SessionSnapshot
	rects     []proto.PaneRect
	screens   map[uint64]*vt.Screen
	sizes     map[uint64]ui.Rect
	focus     uint64
	tab       uint64
	workspace uint64

	sidebar    bool
	grouped    bool
	navigating bool
	nav        navTarget
	// menu is the context menu, open on the thing it acts on. Nil when none.
	menu *ui.Menu
	// staleServer marks that the notice about an older server is up and has
	// the keyboard, because it is asking whether to restart it.
	staleServer bool
	// canRestart marks that replacing the server would help, which it does
	// only when the server is the half that is behind.
	canRestart bool
	// sel is text being marked in a pane, or nil. It belongs to this client:
	// what one person has selected is not part of the session.
	sel *ui.Selection
	// gesture is the pane whose program was given a press, and so is owed the
	// drags and the release that follow it.
	gesture uint64
	// autoScroll is which way the view moves while a drag is held against an
	// edge: -1 back through the history, 1 towards the present.
	autoScroll int
	// spacesScroll and agentsScroll are how far each list is scrolled, in
	// entries. Separate because the lists are: one filling up must not push
	// the other out of sight, which is the whole reason they are divided.
	spacesScroll int
	agentsScroll int
	// sidebarSplit is the line the divider sits on, or zero for "decide for
	// me". Where the user dragged it to is theirs, not the session's.
	sidebarSplit int
	// draggingSidebar marks that the divider is being moved.
	draggingSidebar bool
	// folded names the groups shut in this client's sidebar. Which groups a
	// space belongs to is a session fact; which of them this person has
	// folded away is not, so it is never sent upstream.
	folded map[string]bool

	prompt         promptKind
	promptText     string
	promptPristine bool
	// promptGroup is what a group rename is renaming, since a group has no
	// identifier of its own.
	promptGroup string
	message     string
	alert       bool
	msgAt       time.Time
	overlay     []string
	zoom        bool
	offline     bool
	dirty       bool

	// scrollPane is the pane being looked back through, zero when live.
	// The pane keeps running while it is read: scrolling is a view, not a
	// mode the session is put into.
	scrollPane   uint64
	scrollOffset int
	scrollDepth  int
	scrollScreen *vt.Screen
	// lastFocus is the pane that was focused before this one, for prefix+;.
	// It is noticed while drawing rather than set at every place focus
	// changes: there are eight of those, and the ninth would forget.
	lastFocus uint64
	seenFocus uint64
	// resizing is resize mode: h/j/k/l move the focused pane's edges until
	// escape, without the prefix before every press.
	resizing bool
	// copy is copy mode while it is up, nil otherwise. It sits on the scroll
	// view above: the pane is a still picture and the copy cursor moves
	// through it.
	copy *copyState

	// dragPane and dragSide remember a divider grabbed with the mouse.
	dragPane uint64
	dragSide string
	dragAt   int

	keys     ui.Input
	wake     chan struct{}
	input    chan []byte
	lostConn chan struct{}
	// resync asks the main loop to re-read the session. It holds one slot, so
	// a burst of state changes costs one round trip rather than one each.
	resync chan struct{}

	cols, rows int
	detach     bool
}

func (t *tui) run() error {
	c, err := openSessionOn(t.host, t.session, t)
	if err != nil {
		return err
	}
	t.client = c
	defer c.Close()

	restore, err := enterFullScreen(t.config.UI.Mouse)
	if err != nil {
		return err
	}
	defer restore()

	t.cols, t.rows = terminalCells()
	t.warnIfServerIsOlder()
	if err := t.ensureSession(); err != nil {
		return err
	}
	if err := t.refresh(); err != nil {
		return err
	}

	stopResize := t.watchResize()
	defer stopResize()

	go t.readInput()

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	t.markDirty()
	for {
		select {
		case data, ok := <-t.input:
			if !ok {
				return nil
			}
			if err := t.handleInput(data); err != nil {
				if !t.reportStaleServer(err) {
					return err
				}
			}
			if t.detach {
				return nil
			}

		case <-t.lostConn:
			if t.detach {
				return nil
			}
			if err := t.reconnect(); err != nil {
				return err
			}

		case <-t.resync:
			// Something the session describes changed — which agent a pane is
			// running, what it is doing — and that is not in the pane's output.
			if err := t.refreshSnapshot(); err != nil {
				t.setMessage(err.Error(), true)
			}

		case <-t.wake:
			// A push arrived; the ticker decides when it becomes a frame.

		case <-ticker.C:
			if err := t.autoScrollSelection(); err != nil {
				t.setMessage(err.Error(), true)
			}
			t.expireMessage()
			if err := t.paint(); err != nil {
				return err
			}
		}
	}
}

// reconnect re-establishes the session after the server goes away.
//
// A server restarting is not the client's failure, and a client that exits
// when it happens loses the user's place for a reason that had nothing to do
// with them. Panes are the server's, so after reconnecting everything is
// re-read rather than assumed: the session on the other side may be a
// different one.
func (t *tui) reconnect() error {
	t.setMessage("lost the session; reconnecting…", true)

	deadline := time.Now().Add(reconnectWindow)
	delay := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if t.detach {
			return nil
		}
		c, err := openSessionOn(t.host, t.session, t)
		if err == nil {
			old := t.client
			t.client = c
			if old != nil {
				_ = old.Close()
			}

			t.mu.Lock()
			t.offline = false
			// Nothing about the old session survives: the panes on the other
			// side may be different ones with the same numbers.
			t.screens = make(map[uint64]*vt.Screen)
			t.sizes = make(map[uint64]ui.Rect)
			t.tab, t.focus, t.zoom = 0, 0, false
			t.mu.Unlock()

			t.painter.Invalidate()
			if err := t.ensureSession(); err != nil {
				return err
			}
			if err := t.refresh(); err != nil {
				return err
			}
			t.setMessage("reconnected", false)
			return nil
		}

		// Back off, but not so far that a server which comes straight back
		// leaves the user waiting on an arbitrary timer.
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
	return fmt.Errorf("lost the session %q and could not reconnect", t.session)
}

// --- session setup ---------------------------------------------------------

// ensureSession gives an empty server something to show, so attaching to a
// fresh one lands in a shell rather than on a blank screen with no way to open
// anything.
func (t *tui) ensureSession() error {
	snap, err := t.client.Snapshot()
	if err != nil {
		return err
	}
	if len(snap.Panes) > 0 {
		return nil
	}

	ws := snap.ActiveWorkspace
	if ws == 0 {
		if ws, err = t.client.NewWorkspace("main"); err != nil {
			return err
		}
	}
	_, _, err = t.client.NewTab(ws, "tab 1", proto.PaneSpec{Command: t.config.Shell()})
	return err
}

// refresh re-reads the session and its layout.
//
// Called whenever the shape may have changed — a pane opened or closed, a tab
// switched, the terminal resized — rather than on every frame: the layout is a
// round trip, and panes change shape far less often than they change content.
func (t *tui) refresh() error {
	snap, err := t.client.Snapshot()
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.snap = snap
	t.resolveViewLocked()
	t.revealSidebarLocked()
	tab := t.tab
	t.mu.Unlock()

	if tab == 0 {
		// An empty workspace is a real state, not a failure: the user closed
		// its last tab and is looking at the space itself.
		t.mu.Lock()
		t.rects, t.focus = nil, 0
		t.dirty = true
		t.mu.Unlock()
		t.painter.Invalidate()
		t.markDirty()
		return nil
	}

	area := t.layoutArea()
	layout, err := t.client.TabLayout(tab, area.Cols, area.Rows)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.rects = layout.Panes
	if !paneInLayout(layout.Panes, t.focus) {
		t.focus = firstPane(layout.Panes)
		// The pane being zoomed into is gone, so the zoom goes with it.
		t.zoom = false
	}
	if len(layout.Panes) < 2 {
		t.zoom = false // nothing to zoom away from
	}
	focus := t.focus
	t.mu.Unlock()

	t.mu.Lock()
	effective := t.paneRects()
	t.mu.Unlock()

	if err := t.syncPaneSizes(effective); err != nil {
		return err
	}
	if err := t.subscribe(layout.Panes); err != nil {
		return err
	}
	_ = focus

	t.painter.Invalidate()
	t.markDirty()
	return nil
}

// layoutArea is the region panes are laid out in: the terminal minus the tab
// bar above and the status bar below.
func (t *tui) layoutArea() ui.Rect {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.layoutAreaLocked()
}

// layoutAreaLocked is the region panes are drawn in: the terminal minus the
// tab bar above, the status bar below, and the agent list to the left.
func (t *tui) layoutAreaLocked() ui.Rect {
	top := ui.TabRows(len(t.tabsLocked()))
	// The gutter is the sidebar's width, or the two columns kept for the
	// handle that brings it back. One helper answers for both, so the panes
	// cannot be laid out over something that is drawn.
	left := ui.SidebarGutter(ui.Frame{Sidebar: t.sidebar}, t.cols)
	return ui.Rect{
		X:    left,
		Y:    top,
		Cols: t.cols - left,
		Rows: t.rows - top - ui.StatusRows,
	}
}

// paneRects is where panes actually go, which is the tab's layout unless one
// pane is zoomed and has the area to itself. The caller holds the lock.
func (t *tui) paneRects() []proto.PaneRect {
	area := t.layoutAreaLocked()

	if t.zoom && t.focus != 0 {
		return []proto.PaneRect{{
			Pane: t.focus, X: area.X, Y: area.Y, Cols: area.Cols, Rows: area.Rows,
		}}
	}

	// The layout comes back relative to the area, so it is shifted here rather
	// than the server knowing about a tab bar or an agent list.
	out := make([]proto.PaneRect, len(t.rects))
	for i, r := range t.rects {
		out[i] = r
		out[i].X += area.X
		out[i].Y += area.Y
	}
	return out
}

// syncPaneSizes tells the server what size each pane is being drawn at, so its
// terminal matches the space it has on screen.
func (t *tui) syncPaneSizes(rects []proto.PaneRect) error {
	for _, r := range rects {
		rect := ui.Rect{X: r.X, Y: r.Y, Cols: r.Cols, Rows: r.Rows}
		cols, rows := ui.InnerSize(rect)

		t.mu.Lock()
		previous, known := t.sizes[r.Pane]
		t.sizes[r.Pane] = rect
		screen := t.screens[r.Pane]
		t.mu.Unlock()

		if known && previous == rect && screen != nil {
			continue
		}
		if err := t.client.ResizePane(r.Pane, cols, rows); err != nil {
			return err
		}

		// The pane's own terminal is kept at the size it is drawn at, so what
		// arrives from the server lands where it is meant to.
		t.mu.Lock()
		if screen == nil {
			t.screens[r.Pane] = vt.NewScreen(cols, rows, 0)
		} else {
			screen.Resize(cols, rows)
		}
		t.mu.Unlock()
	}
	return nil
}

func (t *tui) subscribe(rects []proto.PaneRect) error {
	panes := make([]uint64, 0, len(rects))
	for _, r := range rects {
		panes = append(panes, r.Pane)
	}
	return t.client.SubscribePanes(panes)
}

// restartCommand is what stops the server so the next attach starts a new one.
//
// Spelled out in one place because it is the whole point of the message it
// goes in: a notice that names a command which does not do what it says is
// worse than no notice, and "tend kill -s <name>" without -server closes panes
// by number and leaves the server exactly where it was.
func restartCommand(session string) string {
	return "tend kill -s " + session + " -server"
}

// --- server push -----------------------------------------------------------

// Event records a change and wakes the draw loop. It runs on the client's
// reader goroutine, so it does no work beyond that.
func (t *tui) Event(ev proto.Event) {
	switch ev.Kind {
	case proto.EventPaneOpened, proto.EventPaneClosed:
		// The shape changed, and refreshing needs a round trip, so it happens
		// off this goroutine — calling the server from inside its own reply
		// handler would deadlock.
		go func() {
			if err := t.refresh(); err != nil {
				t.setMessage(err.Error(), true)
			}
		}()
	case proto.EventPaneExited:
		go func() {
			if err := t.refresh(); err != nil {
				t.setMessage(err.Error(), true)
			}
		}()
	case proto.EventPaneClipboard:
		t.paneCopied(ev.Data)
	case proto.EventSessionChanged:
		// Another client rearranged or renamed something. Re-read, off this
		// goroutine for the same reason as above.
		go func() {
			if err := t.refresh(); err != nil {
				t.setMessage(err.Error(), true)
			}
		}()
	case proto.EventPaneState:
		// A pane's agent and its state live in the session, not in the bytes
		// the pane produced, so redrawing from what the client already has
		// would show the old answer forever. This is what the agent list is
		// for, and it was silent until the shape of the session happened to
		// change for some other reason.
		t.askResync()
	}
	t.markDirty()
}

// askResync asks the main loop to re-read the session, at most once at a time.
//
// It never blocks: this runs on the client's reader goroutine, and the reply
// to the request it is asking for comes back through that same goroutine.
func (t *tui) askResync() {
	select {
	case t.resync <- struct{}{}:
	default:
	}
}

// refreshSnapshot re-reads the session without recomputing the layout.
//
// A state change cannot move a pane, so paying for a layout round trip on
// every one of them would be work for nothing — and there is one per agent
// per change of what it is doing.
func (t *tui) refreshSnapshot() error {
	snap, err := t.client.Snapshot()
	if err != nil {
		return err
	}

	t.mu.Lock()
	before := t.tab
	t.snap = snap
	t.resolveViewLocked()
	t.revealSidebarLocked()
	changed := t.tab != before
	t.dirty = true
	t.mu.Unlock()

	if changed {
		// The view landed on another tab, so the layout this client is drawing
		// describes panes that are no longer on screen.
		return t.refresh()
	}
	t.wakeUp()
	return nil
}

// Disconnected marks the session as gone and asks the main loop to reconnect.
//
// The reconnect itself happens there rather than here: this runs on the dead
// client's own reader goroutine, and dialling from it would leave the new
// connection owned by a goroutine that is about to end.
func (t *tui) Disconnected() {
	t.mu.Lock()
	t.offline = true
	t.dirty = true
	t.mu.Unlock()

	select {
	case t.lostConn <- struct{}{}:
	default:
	}
	t.wakeUp()
}

// PaneOutput feeds a pane's screen into the copy this client draws from.
func (t *tui) PaneOutput(pane uint64, data []byte) {
	t.mu.Lock()
	screen := t.screens[pane]
	t.mu.Unlock()

	if screen == nil {
		// A pane whose size is not known yet: the next refresh creates it, and
		// the server resends the screen because a screen is a state, not a
		// change.
		return
	}
	t.mu.Lock()
	_, _ = screen.Write(data)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) markDirty() {
	t.mu.Lock()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) wakeUp() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// --- drawing ---------------------------------------------------------------

func (t *tui) paint() error {
	t.mu.Lock()
	if !t.dirty {
		t.mu.Unlock()
		return nil
	}
	t.dirty = false
	frame := t.buildFrame()
	t.mu.Unlock()

	buf := vt.NewGrid(t.cols, t.rows, 0)
	ui.Draw(buf, frame, t.theme)

	x, y, visible := ui.CursorPosition(frame, t.cols, t.rows)
	_, err := os.Stdout.Write(t.painter.Paint(buf, x, y, visible))
	return err
}

// toggleSidebar shows or hides the column, from either the key or the handle.
func (t *tui) toggleSidebar() error {
	t.mu.Lock()
	t.sidebar = !t.sidebar
	if !t.sidebar {
		t.navigating = false
	}
	t.mu.Unlock()
	// The panes change shape, so the screen is redrawn whole rather than
	// patched: every column to the right of the gutter has moved.
	t.painter.Invalidate()
	return t.refresh()
}

// trackPointer turns motion reporting on or off.
//
// Written straight to the terminal rather than through the painter: it is a
// request to the terminal about what to send, not part of the frame, and the
// painter only knows how to describe cells.
func (t *tui) trackPointer(on bool) {
	if !t.config.UI.Mouse {
		return
	}
	seq := ui.DisableMotion
	if on {
		seq = ui.EnableMotion
	}
	_, _ = io.WriteString(os.Stdout, seq)
}

// sessionLabel names the session, and the machine when it is not this one.
//
// Two windows showing "default" look identical, and the one on the production
// host is the one where a mistaken keystroke costs something. The machine is
// said first because it is the part that differs.
func (t *tui) sessionLabel() string {
	if t.host == "" {
		return t.session
	}
	return t.host + ":" + t.session
}

// markSelected puts the navigation cursor on whichever row it points at.
func markSelected(rows []ui.SidebarRow, nav navTarget) {
	for i := range rows {
		target, ok := targetOf(rows[i])
		rows[i].Selected = ok && target == nav
	}
}

// buildFrame assembles what to draw. The caller holds the lock.
func (t *tui) buildFrame() ui.Frame {
	// Noticed here rather than at every place focus is set: there are eight
	// of those and the ninth would forget. The caller holds the lock, which
	// is why this does not take it.
	if t.focus != t.seenFocus {
		t.lastFocus, t.seenFocus = t.seenFocus, t.focus
	}

	frame := ui.Frame{
		Session:   t.sessionLabel(),
		Message:   t.message,
		Alert:     t.alert,
		Prefix:    t.keys.Armed(),
		Overlay:   t.overlay,
		Menu:      t.menu,
		Selection: t.sel,
		Waiting:   t.waitingLocked(),
		Zoomed:    t.zoom,
		Offline:   t.offline,
	}
	if t.prompt != promptNone {
		frame.Prompt = t.promptLabelLocked()
		frame.PromptText = t.promptText
		frame.PromptSelected = t.promptPristine
	}
	if t.scrollPane != 0 {
		frame.Scroll = t.scrollOffset
		frame.ScrollDepth = t.scrollDepth
	}
	frame.Resize = t.resizing
	if t.copy != nil {
		frame.Copy = true
		frame.CopyCursor = t.copyCursorLocked()
		if prompt := t.copyPromptLocked(); prompt != "" {
			frame.Message, frame.Alert = prompt, false
		}
	}

	info := make(map[uint64]proto.PaneInfo, len(t.snap.Panes))
	for _, p := range t.snap.Panes {
		info[p.ID] = p
	}
	if w, ok := t.workspaceLocked(); ok {
		frame.Workspace = w.Name
	}
	for _, tab := range t.tabsLocked() {
		if tab.ID == t.tab {
			frame.Tab = tab.Name
		}
		label := ui.Tab{
			ID:     tab.ID,
			Name:   tab.Name,
			Panes:  len(tab.Panes),
			Active: tab.ID == t.tab,
		}
		// A blocked agent in a tab you are not looking at is the one thing
		// the bar exists to tell you about.
		for _, id := range tab.Panes {
			if info[id].State == "blocked" {
				label.Alert = true
			}
		}
		frame.Tabs = append(frame.Tabs, label)
	}

	if t.sidebar {
		frame.Sidebar = true
		frame.Navigating = t.navigating
		frame.SidebarSplit = t.sidebarSplit
		frame.Spaces = t.spacesSectionLocked()
		frame.Agents = t.agentsSectionLocked()
		if t.navigating {
			markSelected(frame.Spaces.Rows, t.nav)
			markSelected(frame.Agents.Rows, t.nav)
		}
	}

	for _, r := range t.paneRects() {
		p := info[r.Pane]
		frame.Panes = append(frame.Panes, ui.Pane{
			ID:      r.Pane,
			Rect:    ui.Rect{X: r.X, Y: r.Y, Cols: r.Cols, Rows: r.Rows},
			Title:   p.Title,
			Agent:   p.Agent,
			State:   p.State,
			Command: commandName(p.Command),
			Screen:  t.screenFor(r.Pane),
			Running: p.Running,
			Focused: r.Pane == t.focus,
		})
	}
	return frame
}

// toggleOverlay shows the panel, or hides it if the same one is already up,
// so the key that opened the help also closes it.
func (t *tui) toggleOverlay(lines []string) {
	t.mu.Lock()
	if len(t.overlay) > 0 {
		t.overlay = nil
	} else {
		t.overlay = lines
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// dismissOverlay hides the panel when anything else happens, so it never
// covers a pane the user has gone back to using.
func (t *tui) dismissOverlay() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.overlay) == 0 {
		return false
	}
	t.overlay = nil
	t.dirty = true
	return true
}

// screenFor is what to draw for a pane: the live screen, or the scrolled view
// when this client is looking back through it. The caller holds the lock.
func (t *tui) screenFor(pane uint64) *vt.Screen {
	if pane == t.scrollPane && t.scrollScreen != nil {
		return t.scrollScreen
	}
	return t.screens[pane]
}

func (t *tui) setMessage(text string, alert bool) {
	t.mu.Lock()
	t.message, t.alert, t.msgAt, t.dirty = text, alert, time.Now(), true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) expireMessage() {
	t.mu.Lock()
	if t.message != "" && time.Since(t.msgAt) > messageLinger {
		t.message, t.alert, t.dirty = "", false, true
	}
	t.mu.Unlock()
}

// --- input -----------------------------------------------------------------

func (t *tui) readInput() {
	defer close(t.input)
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			t.input <- chunk
		}
		if err != nil {
			return
		}
	}
}

func (t *tui) handleInput(data []byte) error {
	forward, commands, mice := t.keys.FeedAll(data)
	if len(forward) > 0 {
		t.dismissOverlay()
	}
	for _, ev := range mice {
		if err := t.handleMouse(ev); err != nil {
			t.setMessage(err.Error(), true)
		}
	}

	t.mu.Lock()
	focus := t.focus
	offline := t.offline
	t.dirty = true // the prefix indicator may have changed
	t.mu.Unlock()

	if offline {
		// Keystrokes have nowhere to go, and a command would only fail. The
		// exception is detaching, which is the user asking to stop waiting.
		for _, action := range commands {
			if action.Command == ui.CommandDetach {
				t.detach = true
				return nil
			}
		}
		return nil
	}

	// A chunk can hold both keys and commands — "q" leaving the scroll view
	// and the prefix sequence after it often arrive together — so consuming
	// the keys must not discard the commands that came with them.
	// The prompt takes the keyboard ahead of everything else: while a name is
	// being typed, every key is part of that name.
	// The stale-server notice takes the keyboard before anything else: it is
	// over the screen, and it is asking a question.
	if t.staleServerUp() {
		handled, err := t.staleServerKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
			commands = nil
		}
	}

	// An open menu takes the keyboard first: it is the thing on top of the
	// screen, and a key going past it to a pane would be typed into something
	// the user cannot see.
	if t.menuOpen() {
		handled, err := t.menuKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
		}
	}

	if t.prompting() {
		handled, err := t.promptKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
		}
	}

	if t.navigatingNow() {
		handled, err := t.navigateKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
		}
	}

	if t.resizingNow() {
		handled, err := t.resizeKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
		}
	}

	if t.copying() {
		// Before the scroll view's keys: copy mode is on top of it, and every
		// key is copy mode's while it is up.
		if _, err := t.copyKeys(forward); err != nil {
			return err
		}
		forward = nil
	}

	if t.scrolling() {
		handled, err := t.scrollKeys(forward)
		if err != nil {
			return err
		}
		if handled {
			forward = nil
		}
	}

	if len(forward) > 0 && focus != 0 && !t.scrolling() {
		if err := t.client.SendInput(focus, forward); err != nil {
			return err
		}
	}
	for _, action := range commands {
		if err := t.command(action); err != nil {
			t.setMessage(err.Error(), true)
		}
		if t.detach {
			return nil
		}
	}
	return nil
}

func (t *tui) command(action ui.Action) error {
	cmd := action.Command

	t.mu.Lock()
	focus := t.focus
	tab := t.tab
	rects := t.rects
	t.mu.Unlock()

	switch cmd {
	case ui.CommandSelectTab:
		return t.selectTab(action.Arg)

	case ui.CommandNextSpace, ui.CommandPrevSpace:
		return t.switchWorkspace(cmd == ui.CommandNextSpace)

	case ui.CommandNewSpace:
		return t.newWorkspace()

	case ui.CommandToggleAgents:
		return t.toggleSidebar()

	case ui.CommandMenu:
		if t.menuOpen() {
			t.closeMenu()
			return nil
		}
		// From the keyboard the menu opens on the focused pane, at its top
		// corner: there is no pointer to open it under.
		if focus == 0 {
			return nil
		}
		at := ui.Rect{}
		for _, r := range rects {
			if r.Pane == focus {
				at = ui.Rect{X: r.X, Y: r.Y}
			}
		}
		t.openMenu(ui.PaneMenu(focus, at.X+2, at.Y+1, len(rects) > 1))
		return nil

	case ui.CommandRenameTab:
		t.startPrompt(promptRenameTab)
		return nil

	case ui.CommandRenameSpace:
		t.startPrompt(promptRenameSpace)
		return nil

	case ui.CommandNavigate:
		if t.navigatingNow() {
			t.leaveNavigate()
			return nil
		}
		t.enterNavigate()
		return t.refresh()

	case ui.CommandSplitColumns, ui.CommandSplitRows:
		if focus == 0 {
			return nil
		}
		dir := "columns"
		if cmd == ui.CommandSplitRows {
			dir = "rows"
		}
		created, err := t.client.SplitPane(focus, dir, proto.PaneSpec{Command: t.config.Shell()})
		if err != nil {
			return err
		}
		// Focus follows the split, as it does in every other multiplexer:
		// splitting is how you make a pane to use, not one to look at.
		t.mu.Lock()
		t.focus = created
		t.mu.Unlock()
		return t.refresh()

	case ui.CommandFocusLeft, ui.CommandFocusRight, ui.CommandFocusUp, ui.CommandFocusDown:
		t.moveFocus(sideFor(cmd), rects, focus)
		return nil

	case ui.CommandFocusNext:
		t.focusStep(rects, focus, 1)
		return nil

	case ui.CommandFocusPrev:
		t.focusStep(rects, focus, -1)
		return nil

	case ui.CommandLastPane:
		t.mu.Lock()
		last := t.lastFocus
		t.mu.Unlock()
		if last == 0 || last == focus {
			return nil
		}
		return t.jumpToPane(last)

	case ui.CommandPrevAgent, ui.CommandNextAgent:
		step := 1
		if cmd == ui.CommandPrevAgent {
			step = -1
		}
		next := t.agentStep(focus, step)
		if next == 0 {
			t.setMessage("no agents in this session", false)
			return nil
		}
		return t.jumpToPane(next)

	case ui.CommandScroll:
		// prefix+[ is copy mode, as in tmux and herdr. It used to be a plain
		// scroll view, which copy mode now is with a cursor added.
		if t.copying() {
			t.leaveCopy()
			return nil
		}
		if t.scrolling() {
			t.leaveScroll()
			return nil
		}
		return t.enterCopy()

	case ui.CommandZoom:
		t.mu.Lock()
		t.zoom = !t.zoom
		t.mu.Unlock()
		return t.refresh()

	case ui.CommandGrowLeft, ui.CommandGrowRight, ui.CommandGrowUp, ui.CommandGrowDown:
		if focus == 0 {
			return nil
		}
		area := t.layoutArea()
		if err := t.client.AdjustSplit(focus, growSide(cmd), resizeStep, area.Cols, area.Rows); err != nil {
			return err
		}
		return t.refresh()

	case ui.CommandClosePane:
		if focus == 0 {
			return nil
		}
		if err := t.client.ClosePane(focus); err != nil {
			return err
		}
		return t.refresh()

	case ui.CommandNewTab:
		return t.newTabHere()

	case ui.CommandNextTab, ui.CommandPrevTab:
		t.switchTab(cmd == ui.CommandNextTab, tab)
		return t.refresh()

	case ui.CommandDetach:
		t.detach = true
		return nil

	case ui.CommandRefresh:
		t.painter.Invalidate()
		return t.refresh()

	case ui.CommandSwapLeft, ui.CommandSwapRight, ui.CommandSwapUp, ui.CommandSwapDown:
		if focus == 0 {
			return nil
		}
		area := t.layoutArea()
		if _, err := t.client.SwapPaneToward(focus, swapSide(cmd), area.Cols, area.Rows); err != nil {
			if t.reportStaleServer(err) {
				return nil
			}
			// Nothing on that side is not a failure worth a message: it is
			// the edge of the tab, and the key did what it could.
			if isNothingToMove(err) {
				return nil
			}
			return err
		}
		// Focus is on the pane, not the place, so it went with the pane.
		return t.refresh()

	case ui.CommandResizeMode:
		t.mu.Lock()
		t.resizing = !t.resizing
		t.dirty = true
		t.mu.Unlock()
		return nil

	case ui.CommandHelp:
		t.toggleOverlay(ui.HelpLines())
		return nil
	}
	return nil
}

// isNothingToMove recognises the server's session.ErrNoMove, which arrives as
// text over the wire behind the method's name: "pane.swap: session: nothing
// to move". The message is the only thing that crosses.
func isNothingToMove(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), session.ErrNoMove.Error())
}

// swapSide names the side a swap key trades toward.
func swapSide(cmd ui.Command) string {
	switch cmd {
	case ui.CommandSwapLeft:
		return "left"
	case ui.CommandSwapRight:
		return "right"
	case ui.CommandSwapUp:
		return "up"
	default:
		return "down"
	}
}

// growSide names the edge a resize key moves.
func growSide(cmd ui.Command) string {
	switch cmd {
	case ui.CommandGrowLeft:
		return "left"
	case ui.CommandGrowRight:
		return "right"
	case ui.CommandGrowUp:
		return "up"
	default:
		return "down"
	}
}

func sideFor(cmd ui.Command) session.Side {
	switch cmd {
	case ui.CommandFocusLeft:
		return session.Left
	case ui.CommandFocusRight:
		return session.Right
	case ui.CommandFocusUp:
		return session.Up
	default:
		return session.Down
	}
}

// moveFocus picks the neighbouring pane.
//
// The geometry lives in the session package and is reused rather than
// reimplemented: which pane is "to the left" has a careful answer involving
// edge distance and shared border, and having two of them would mean having
// two that disagree.
func (t *tui) moveFocus(side session.Side, rects []proto.PaneRect, focus uint64) {
	converted := make([]session.PaneRect, 0, len(rects))
	for _, r := range rects {
		converted = append(converted, session.PaneRect{
			Pane: session.PaneID(r.Pane),
			Rect: session.Rect{X: r.X, Y: r.Y, W: r.Cols, H: r.Rows},
		})
	}
	next, ok := session.Neighbor(converted, session.PaneID(focus), side)
	if !ok {
		return
	}
	t.mu.Lock()
	t.focus = uint64(next)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// focusStep cycles through the tab's panes, forwards or back.
func (t *tui) focusStep(rects []proto.PaneRect, focus uint64, step int) {
	if len(rects) == 0 {
		return
	}
	next := rects[0].Pane
	for i, r := range rects {
		if r.Pane == focus {
			next = rects[(i+step+len(rects))%len(rects)].Pane
			break
		}
	}
	t.mu.Lock()
	t.focus = next
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) navigatingNow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.navigating
}

// shownWorkspace is the workspace being looked at, which is where a new tab
// belongs — not whichever one the server last considered active.
func (t *tui) shownWorkspace() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.workspace != 0 {
		return t.workspace
	}
	if t.snap.ActiveWorkspace != 0 {
		return t.snap.ActiveWorkspace
	}
	if len(t.snap.Workspaces) > 0 {
		return t.snap.Workspaces[0].ID
	}
	return 0
}

// switchTab moves within the workspace being shown.
//
// Only within it: a tab in another space is not the next tab, it is somewhere
// else, and stepping into it without saying so would leave the user unsure
// where they are.
func (t *tui) switchTab(forward bool, current uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tabs := t.tabsLocked()
	if len(tabs) < 2 {
		return
	}
	idx := 0
	for i, tab := range tabs {
		if tab.ID == current {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(tabs)
	} else {
		idx = (idx - 1 + len(tabs)) % len(tabs)
	}
	t.tab = tabs[idx].ID
	t.focus, t.zoom = 0, false
}

// --- terminal --------------------------------------------------------------

// enterFullScreen puts the terminal into the state a full-screen application
// needs, and returns the function that undoes every part of it.
//
// The alternate screen is what makes detaching leave the shell as it was
// rather than buried under a session's worth of output.
func enterFullScreen(mouse bool) (func(), error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("attach: raw mode: %w", err)
	}
	io.WriteString(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
	if mouse {
		io.WriteString(os.Stdout, ui.EnableMouse)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			// Mouse reporting is always turned off, even if it was never
			// turned on: a terminal left reporting makes every later click in
			// that window emit gibberish.
			io.WriteString(os.Stdout, ui.DisableMotion+ui.DisableMouse+"\x1b[0m\x1b[?25h\x1b[?1049l")
			_ = term.Restore(fd, state)
		})
	}, nil
}

func terminalCells() (cols, rows int) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols < 1 || rows < 1 {
		return 80, 24
	}
	return cols, rows
}

func (t *tui) watchResize() func() {
	ch := make(chan os.Signal, 1)
	if !notifyResize(ch) {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ch:
				t.cols, t.rows = terminalCells()
				t.painter.Invalidate()
				if err := t.refresh(); err != nil {
					t.setMessage(err.Error(), true)
				}
			}
		}
	}()
	return func() { close(done) }
}

// --- small helpers ---------------------------------------------------------

func tabExists(snap proto.SessionSnapshot, id uint64) bool {
	for _, w := range snap.Workspaces {
		for _, tab := range w.Tabs {
			if tab.ID == id {
				return true
			}
		}
	}
	return false
}

func activeTab(snap proto.SessionSnapshot) uint64 {
	for _, w := range snap.Workspaces {
		if w.ID == snap.ActiveWorkspace && w.ActiveTab != 0 {
			return w.ActiveTab
		}
	}
	for _, w := range snap.Workspaces {
		if len(w.Tabs) > 0 {
			return w.Tabs[0].ID
		}
	}
	return 0
}

func paneInLayout(rects []proto.PaneRect, pane uint64) bool {
	for _, r := range rects {
		if r.Pane == pane {
			return true
		}
	}
	return false
}

func firstPane(rects []proto.PaneRect) uint64 {
	if len(rects) == 0 {
		return 0
	}
	return rects[0].Pane
}
