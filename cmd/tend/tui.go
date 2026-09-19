package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/sousaakira/tend/internal/client"
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

// runAttach draws a session and forwards keys to it.
func runAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ExitOnError)
	name := sessionFlag(fs)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend attach [-s session]\n\n"+
				"draws a session's panes and forwards the keyboard to the focused one.\n"+
				"ctrl+b is the prefix; ctrl+b ? lists the keys. ctrl+b d detaches,\n"+
				"leaving everything running.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("attach needs a terminal; use \"tend follow\" when output is redirected")
	}

	t := &tui{
		session: *name,
		theme:   ui.DefaultTheme(),
		painter: vt.NewPainter(),
		screens: make(map[uint64]*vt.Screen),
		sizes:   make(map[uint64]ui.Rect),
		wake:    make(chan struct{}, 1),
		input:   make(chan []byte, 64),
	}
	return t.run()
}

// tui is one attached client.
//
// The server pushes from its own goroutine, so everything it delivers is
// recorded under this lock and the draw loop is only woken. Drawing from the
// delivering goroutine would put the screen at the mercy of how fast a pane
// produces output.
type tui struct {
	session string
	theme   ui.Theme
	client  *client.Client
	painter *vt.Painter

	mu      sync.Mutex
	snap    proto.SessionSnapshot
	rects   []proto.PaneRect
	screens map[uint64]*vt.Screen
	sizes   map[uint64]ui.Rect
	focus   uint64
	tab     uint64
	message string
	alert   bool
	msgAt   time.Time
	overlay []string
	dirty   bool

	keys  ui.Input
	wake  chan struct{}
	input chan []byte

	cols, rows int
	detach     bool
}

func (t *tui) run() error {
	c, err := openSession(t.session, t)
	if err != nil {
		return err
	}
	t.client = c
	defer c.Close()

	restore, err := enterFullScreen()
	if err != nil {
		return err
	}
	defer restore()

	t.cols, t.rows = terminalCells()
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
				return err
			}
			if t.detach {
				return nil
			}

		case <-t.wake:
			// A push arrived; the ticker decides when it becomes a frame.

		case <-ticker.C:
			t.expireMessage()
			if err := t.paint(); err != nil {
				return err
			}
		}
	}
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
	_, _, err = t.client.NewTab(ws, "shell", proto.PaneSpec{Command: defaultShell()})
	return err
}

func defaultShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return []string{path}
	}
	return []string{"/bin/sh"}
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
	tab := t.tab
	t.mu.Unlock()

	if tab == 0 || !tabExists(snap, tab) {
		tab = activeTab(snap)
	}
	if tab == 0 {
		t.mu.Lock()
		t.tab, t.rects, t.focus = 0, nil, 0
		t.dirty = true
		t.mu.Unlock()
		return nil
	}

	area := t.layoutArea()
	layout, err := t.client.TabLayout(tab, area.Cols, area.Rows)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.tab = tab
	t.rects = layout.Panes
	if !paneInLayout(layout.Panes, t.focus) {
		t.focus = firstPane(layout.Panes)
	}
	focus := t.focus
	t.mu.Unlock()

	if err := t.syncPaneSizes(layout.Panes); err != nil {
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

// layoutArea is the region panes are laid out in: the terminal minus the
// status bar.
func (t *tui) layoutArea() ui.Rect {
	return ui.Rect{Cols: t.cols, Rows: t.rows - ui.StatusRows}
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
	}
	t.markDirty()
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

	x, y, visible := ui.CursorPosition(frame)
	_, err := os.Stdout.Write(t.painter.Paint(buf, x, y, visible))
	return err
}

// buildFrame assembles what to draw. The caller holds the lock.
func (t *tui) buildFrame() ui.Frame {
	frame := ui.Frame{
		Session: t.session,
		Message: t.message,
		Alert:   t.alert,
		Prefix:  t.keys.Armed(),
		Overlay: t.overlay,
	}

	info := make(map[uint64]proto.PaneInfo, len(t.snap.Panes))
	for _, p := range t.snap.Panes {
		info[p.ID] = p
	}
	for _, w := range t.snap.Workspaces {
		if w.ID == t.snap.ActiveWorkspace {
			frame.Workspace = w.Name
		}
		for _, tab := range w.Tabs {
			if tab.ID == t.tab {
				frame.Tab = tab.Name
			}
		}
	}

	for _, r := range t.rects {
		p := info[r.Pane]
		frame.Panes = append(frame.Panes, ui.Pane{
			ID:      r.Pane,
			Rect:    ui.Rect{X: r.X, Y: r.Y, Cols: r.Cols, Rows: r.Rows},
			Title:   p.Title,
			Agent:   p.Agent,
			State:   p.State,
			Command: commandName(p.Command),
			Screen:  t.screens[r.Pane],
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
	forward, commands := t.keys.FeedAll(data)
	if len(forward) > 0 {
		t.dismissOverlay()
	}

	t.mu.Lock()
	focus := t.focus
	t.dirty = true // the prefix indicator may have changed
	t.mu.Unlock()

	if len(forward) > 0 && focus != 0 {
		if err := t.client.SendInput(focus, forward); err != nil {
			return err
		}
	}
	for _, cmd := range commands {
		if err := t.command(cmd); err != nil {
			t.setMessage(err.Error(), true)
		}
		if t.detach {
			return nil
		}
	}
	return nil
}

func (t *tui) command(cmd ui.Command) error {
	t.mu.Lock()
	focus := t.focus
	tab := t.tab
	rects := t.rects
	t.mu.Unlock()

	switch cmd {
	case ui.CommandSplitColumns, ui.CommandSplitRows:
		if focus == 0 {
			return nil
		}
		dir := "columns"
		if cmd == ui.CommandSplitRows {
			dir = "rows"
		}
		created, err := t.client.SplitPane(focus, dir, proto.PaneSpec{Command: defaultShell()})
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
		t.focusNext(rects, focus)
		return nil

	case ui.CommandClosePane:
		if focus == 0 {
			return nil
		}
		if err := t.client.ClosePane(focus); err != nil {
			return err
		}
		return t.refresh()

	case ui.CommandNewTab:
		ws := t.activeWorkspace()
		if ws == 0 {
			return nil
		}
		newTab, _, err := t.client.NewTab(ws, "shell", proto.PaneSpec{Command: defaultShell()})
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.tab = newTab
		t.mu.Unlock()
		return t.refresh()

	case ui.CommandNextTab, ui.CommandPrevTab:
		t.switchTab(cmd == ui.CommandNextTab, tab)
		return t.refresh()

	case ui.CommandDetach:
		t.detach = true
		return nil

	case ui.CommandRefresh:
		t.painter.Invalidate()
		return t.refresh()

	case ui.CommandHelp:
		t.toggleOverlay(ui.HelpLines())
		return nil
	}
	return nil
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

func (t *tui) focusNext(rects []proto.PaneRect, focus uint64) {
	if len(rects) == 0 {
		return
	}
	next := rects[0].Pane
	for i, r := range rects {
		if r.Pane == focus {
			next = rects[(i+1)%len(rects)].Pane
			break
		}
	}
	t.mu.Lock()
	t.focus = next
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) activeWorkspace() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.snap.ActiveWorkspace != 0 {
		return t.snap.ActiveWorkspace
	}
	if len(t.snap.Workspaces) > 0 {
		return t.snap.Workspaces[0].ID
	}
	return 0
}

func (t *tui) switchTab(forward bool, current uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var tabs []uint64
	for _, w := range t.snap.Workspaces {
		for _, tab := range w.Tabs {
			tabs = append(tabs, tab.ID)
		}
	}
	if len(tabs) < 2 {
		return
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i] < tabs[j] })

	idx := 0
	for i, id := range tabs {
		if id == current {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(tabs)
	} else {
		idx = (idx - 1 + len(tabs)) % len(tabs)
	}
	t.tab = tabs[idx]
	t.focus = 0
}

// --- terminal --------------------------------------------------------------

// enterFullScreen puts the terminal into the state a full-screen application
// needs, and returns the function that undoes every part of it.
//
// The alternate screen is what makes detaching leave the shell as it was
// rather than buried under a session's worth of output.
func enterFullScreen() (func(), error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("attach: raw mode: %w", err)
	}
	io.WriteString(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")

	var once sync.Once
	return func() {
		once.Do(func() {
			io.WriteString(os.Stdout, "\x1b[0m\x1b[?25h\x1b[?1049l")
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
