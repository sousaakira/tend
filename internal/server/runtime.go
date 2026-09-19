package server

import (
	"sync"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/vt"
)

// paneRuntime is one pane's live half: its process, its terminal, and the
// detector reading it. The session record holding the pane's identity and
// place in the layout lives separately, and the two are paired by id.
//
// Its mutex guards only this pane. The server's lock guards the session
// structure. Lock order is always server then runtime, never the reverse —
// which is why the terminal's own callbacks record into the runtime and let
// the detection loop carry the result up, rather than reaching for the
// session while a pane is parsing bytes.
type paneRuntime struct {
	id      session.PaneID
	agentID string

	pty *pty.Pty

	mu       sync.Mutex
	screen   *vt.Screen
	detector *agent.Detector
	dirty    bool
	title    string
	running  bool
	closing  bool
	exitErr  string
}

func newPaneRuntime(id session.PaneID, p *pty.Pty, size pty.Size, manifest *detect.Manifest) *paneRuntime {
	rt := &paneRuntime{
		id:      id,
		pty:     p,
		screen:  vt.NewScreen(int(size.Cols), int(size.Rows), scrollbackLines),
		running: true,
	}
	if manifest != nil {
		rt.agentID = manifest.ID
		rt.detector = agent.NewDetector(manifest)
	}
	// The terminal reports a title from inside Write, which runs under this
	// runtime's lock. Recording it here and letting the detection loop apply
	// it keeps that callback from reaching for the session lock.
	rt.screen.OnTitle = func(title string) { rt.title = title }
	return rt
}

// write feeds terminal output to the screen.
func (rt *paneRuntime) write(b []byte) {
	rt.mu.Lock()
	_, _ = rt.screen.Write(b)
	rt.dirty = true
	rt.mu.Unlock()
}

// observation is what one poll of a pane found.
type observation struct {
	title        string
	titleChanged bool

	state        detect.State
	rule         string
	stateChanged bool
}

// poll runs detection if the screen changed since the last look.
//
// An unchanged screen costs nothing: parsing output is cheap, re-running every
// rule over it is not, and this runs per pane on every tick.
func (rt *paneRuntime) poll(lastTitle string) observation {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var obs observation
	if rt.title != lastTitle {
		obs.title = rt.title
		obs.titleChanged = true
	}
	if !rt.dirty || rt.detector == nil {
		return obs
	}
	rt.dirty = false

	res, changed := rt.detector.Update(rt.screen)
	if changed {
		obs.state = res.State
		obs.rule = res.RuleID
		obs.stateChanged = true
	}
	return obs
}

// resize changes the terminal and tells the process about it.
func (rt *paneRuntime) resize(size pty.Size) error {
	rt.mu.Lock()
	rt.screen.Resize(int(size.Cols), int(size.Rows))
	rt.mu.Unlock()
	// Resizing the pty delivers SIGWINCH, so it must happen outside the lock:
	// the process may write its redraw before the call returns, and that write
	// needs the lock we would otherwise still be holding.
	return rt.pty.Resize(size)
}

// setClosing records that tend is stopping this pane on purpose, so that the
// hangup it is about to receive is not reported as the process failing.
func (rt *paneRuntime) setClosing() {
	rt.mu.Lock()
	rt.closing = true
	rt.mu.Unlock()
}

// markExited records that the process ended, and reports whether the exit was
// deliberate.
func (rt *paneRuntime) markExited(err error) (deliberate bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.running = false
	if rt.closing {
		return true
	}
	if err != nil {
		rt.exitErr = err.Error()
	}
	return false
}

// status is a copy of what a client needs to show about the pane.
func (rt *paneRuntime) status() PaneStatus {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	st := PaneStatus{
		ID:      rt.id,
		Title:   rt.title,
		Agent:   rt.agentID,
		Running: rt.running,
		ExitErr: rt.exitErr,
		Pid:     rt.pty.Pid(),
	}
	if rt.detector != nil {
		st.State = rt.detector.State()
		st.Rule = rt.detector.Rule()
	}
	return st
}

// screenText returns the pane's screen as plain text, which is what the CLI
// and the detector read.
func (rt *paneRuntime) screenText() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return agent.ScreenText(rt.screen)
}

// withScreen runs fn against the pane's terminal while holding its lock.
//
// It exists so a renderer can read cells without the screen being copied, and
// it is the only way out of this package to the terminal. fn must not block,
// call back into the server, or keep the screen: every byte the pane produces
// waits behind it.
func (rt *paneRuntime) withScreen(fn func(*vt.Screen)) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	fn(rt.screen)
}
