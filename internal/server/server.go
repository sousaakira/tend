// Package server owns a running tend session.
//
// It pairs the pure records in internal/session with the live halves they
// describe: a pty, a terminal, and a detector per pane. The split is the point
// — a pane's identity and place in the layout outlive its process, and the
// session can be reasoned about without any of this running.
//
// # Locking
//
// Two levels, always taken in this order and never the reverse:
//
//  1. the server's lock, guarding the session tree and the runtime map
//  2. a pane runtime's own lock, guarding that pane's terminal
//
// Pane output never touches the server's lock, which is what keeps one busy
// agent from serialising every other pane through a single mutex. Callbacks
// from inside a terminal record into the runtime and let the detection loop
// carry the result up.
package server

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/vt"
)

const (
	// scrollbackLines is how much history a pane keeps.
	scrollbackLines = 5000
	// readBuffer is the chunk size for pty reads. Agent output arrives in
	// bursts of full redraws, so a small buffer costs syscalls for nothing.
	readBuffer = 32 * 1024
	// defaultDetectInterval is how often panes are re-examined. Detection is
	// skipped entirely for a pane whose screen has not changed, so this bounds
	// how stale a state can be rather than how much work is done.
	defaultDetectInterval = 150 * time.Millisecond
	// defaultShutdownGrace is how long a pane gets to end on a hangup before
	// it is killed. A process that ignores SIGHUP must not be able to hold
	// shutdown open indefinitely.
	defaultShutdownGrace = 2 * time.Second
)

// ErrClosed is returned once the server has shut down.
var ErrClosed = errors.New("server: closed")

// Config configures a server. The zero value is usable.
type Config struct {
	// Catalog supplies the detection manifests. Nil loads the bundled ones.
	Catalog *detect.Catalog
	// DetectInterval is how often panes are re-examined. Zero picks a default.
	DetectInterval time.Duration
	// DefaultSize is the terminal size a pane starts at, before a client
	// attaches and resizes it.
	DefaultSize pty.Size
	// ShutdownGrace is how long Close waits for panes to end on their own
	// before killing them. Zero picks a default.
	ShutdownGrace time.Duration
}

// PaneSpec describes a pane to open.
type PaneSpec struct {
	Command []string
	Dir     string
	Env     []string
	Title   string

	// Agent names the detection manifest. Empty means infer it from the
	// command; a command that matches nothing simply gets no detector, since
	// plenty of useful panes are not agents.
	Agent string

	// Size is the pane's initial terminal size. Zero uses the server default.
	Size pty.Size
}

// PaneStatus is what a client needs to show about a pane.
type PaneStatus struct {
	ID      session.PaneID
	Title   string
	Agent   string
	State   detect.State
	Rule    string
	Running bool
	Pid     int
	ExitErr string
}

// Server owns a running session.
type Server struct {
	cfg     Config
	catalog *detect.Catalog
	events  *eventHub

	mu       sync.Mutex
	session  *session.Session
	runtimes map[session.PaneID]*paneRuntime
	titles   map[session.PaneID]string
	// conns tracks connected clients so shutdown can hang them up. Without
	// this they sit blocked on a socket read that nothing ever ends, and Close
	// waits on them forever.
	conns  map[*clientConn]struct{}
	closed bool

	done chan struct{}
	wg   sync.WaitGroup
}

// New starts a server with no panes.
func New(cfg Config) (*Server, error) {
	catalog := cfg.Catalog
	if catalog == nil {
		var err error
		catalog, err = detect.Bundled()
		if err != nil {
			return nil, fmt.Errorf("server: loading manifests: %w", err)
		}
	}
	if cfg.DetectInterval <= 0 {
		cfg.DetectInterval = defaultDetectInterval
	}
	if !cfg.DefaultSize.Valid() {
		cfg.DefaultSize = pty.DefaultSize
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = defaultShutdownGrace
	}

	s := &Server{
		cfg:      cfg,
		catalog:  catalog,
		events:   newEventHub(),
		session:  session.New(),
		runtimes: make(map[session.PaneID]*paneRuntime),
		titles:   make(map[session.PaneID]string),
		conns:    make(map[*clientConn]struct{}),
		done:     make(chan struct{}),
	}

	s.wg.Add(1)
	go s.detectLoop()
	return s, nil
}

// Close stops every pane and ends every subscription.
//
// Each pane is hung up, which ends its process and so ends the goroutine
// reading it — closing the terminal alone would not, since a read already
// blocked on the master is a plain syscall Go cannot interrupt. A pane that
// ignores the hangup is killed once the grace period runs out.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	close(s.done)
	for _, rt := range runtimes {
		rt.setClosing()
		_ = rt.pty.Close()
	}
	// Hang up the clients too. Their handlers are blocked on a socket read
	// that nothing else will end, and a client seeing its connection drop is
	// how it learns the server is gone.
	for _, c := range conns {
		_ = c.conn.Close()
	}

	// A pane that ignores the hangup gets killed rather than holding shutdown
	// open. Without this, one stubborn process would block Close forever.
	if !awaitGroup(&s.wg, s.cfg.ShutdownGrace) {
		for _, rt := range runtimes {
			_ = rt.pty.Kill()
		}
		s.wg.Wait()
	}

	s.events.closeAll()
	return nil
}

// awaitGroup reports whether wg finished within d.
func awaitGroup(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// Subscribe returns a stream of events. The caller closes it when done.
func (s *Server) Subscribe(buffer int) *Subscription { return s.events.subscribe(buffer) }

// --- session structure -----------------------------------------------------

// NewWorkspace adds a workspace and focuses it.
func (s *Server) NewWorkspace(name string) (session.WorkspaceID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	return s.session.AddWorkspace(name).ID, nil
}

// NewTab creates a tab with one pane and starts its process.
//
// The pane is started while the session lock is held. Starting a process is
// brief and, unlike pane output, does not sit on a hot path; holding the lock
// means no caller can ever observe a pane record without the runtime that
// backs it.
func (s *Server) NewTab(ws session.WorkspaceID, name string, spec PaneSpec) (session.TabID, session.PaneID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, 0, ErrClosed
	}

	tab, pane, err := s.session.AddTab(ws, name, spec.record())
	if err != nil {
		return 0, 0, err
	}
	if err := s.startLocked(pane.ID, spec); err != nil {
		// Roll the session back: a tab whose only pane never started is not a
		// tab the user asked for.
		_, _ = s.session.CloseTab(tab.ID)
		return 0, 0, err
	}
	return tab.ID, pane.ID, nil
}

// SplitPane divides a pane and starts a process in the new half.
func (s *Server) SplitPane(target session.PaneID, dir session.Direction, spec PaneSpec) (session.PaneID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}

	pane, err := s.session.SplitPane(target, dir, spec.record())
	if err != nil {
		return 0, err
	}
	if err := s.startLocked(pane.ID, spec); err != nil {
		_ = s.session.ClosePane(pane.ID)
		return 0, err
	}
	return pane.ID, nil
}

// ClosePane stops a pane's process and removes it from the session.
func (s *Server) ClosePane(id session.PaneID) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	rt, ok := s.runtimes[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	if err := s.session.ClosePane(id); err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.runtimes, id)
	delete(s.titles, id)
	s.mu.Unlock()

	rt.setClosing()
	_ = rt.pty.Close()
	s.events.publish(Event{Kind: EventPaneClosed, Pane: id})
	return nil
}

// CloseTab stops every pane in a tab and removes it.
func (s *Server) CloseTab(id session.TabID) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	closed, err := s.session.CloseTab(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stopped := make([]*paneRuntime, 0, len(closed))
	for _, pid := range closed {
		if rt, ok := s.runtimes[pid]; ok {
			stopped = append(stopped, rt)
		}
		delete(s.runtimes, pid)
		delete(s.titles, pid)
	}
	s.mu.Unlock()

	for _, rt := range stopped {
		rt.setClosing()
		_ = rt.pty.Close()
		s.events.publish(Event{Kind: EventPaneClosed, Pane: rt.id})
	}
	return nil
}

// startLocked opens a pty for an existing pane record. The caller holds the
// session lock.
func (s *Server) startLocked(id session.PaneID, spec PaneSpec) error {
	if len(spec.Command) == 0 {
		return errors.New("server: pane has no command")
	}

	manifest, err := agent.ResolveManifest(s.catalog, spec.Agent, spec.Command[0])
	if err != nil {
		return err
	}

	size := spec.Size
	if !size.Valid() {
		size = s.cfg.DefaultSize
	}

	p, err := pty.Start(spec.Command[0], spec.Command[1:], pty.Options{
		Dir:  spec.Dir,
		Env:  spec.Env,
		Size: size,
	})
	if err != nil {
		return fmt.Errorf("server: starting %s: %w", spec.Command[0], err)
	}

	rt := newPaneRuntime(id, p, size, manifest)
	s.runtimes[id] = rt
	s.titles[id] = ""
	if manifest != nil {
		_ = s.session.SetPaneState(id, manifest.ID, detect.StateUnknown)
	}

	s.wg.Add(1)
	go s.readPane(rt)

	s.events.publish(Event{Kind: EventPaneOpened, Pane: id})
	return nil
}

// record converts a spec into the session's own pane description.
func (spec PaneSpec) record() session.PaneSpec {
	return session.PaneSpec{
		Command: spec.Command,
		Dir:     spec.Dir,
		Title:   spec.Title,
		Agent:   spec.Agent,
	}
}

// --- pane input and output -------------------------------------------------

// Write sends input to a pane, as though it had been typed.
func (s *Server) Write(id session.PaneID, data []byte) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	_, err = rt.pty.Write(data)
	return err
}

// Resize changes a pane's terminal size.
func (s *Server) Resize(id session.PaneID, size pty.Size) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	if !size.Valid() {
		return nil
	}
	return rt.resize(size)
}

// ScreenText returns a pane's screen as plain text.
func (s *Server) ScreenText(id session.PaneID) (string, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return "", err
	}
	return rt.screenText(), nil
}

// WithScreen runs fn against a pane's terminal.
//
// It is how a renderer reads cells without copying the screen. fn must not
// block or call back into the server: every byte that pane produces waits
// behind it.
func (s *Server) WithScreen(id session.PaneID, fn func(*vt.Screen)) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	rt.withScreen(fn)
	return nil
}

// PaneStatus reports what a client needs to show about a pane.
func (s *Server) PaneStatus(id session.PaneID) (PaneStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return PaneStatus{}, err
	}
	return rt.status(), nil
}

// Statuses reports every pane, in no particular order.
func (s *Server) Statuses() []PaneStatus {
	s.mu.Lock()
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	s.mu.Unlock()

	out := make([]PaneStatus, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, rt.status())
	}
	return out
}

// Session runs fn against the session tree while holding the server's lock.
//
// The session is not safe for concurrent use, so this is the only way to read
// it. fn must not block or call back into the server, and must not keep the
// pointer.
func (s *Server) Session(fn func(*session.Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.session)
}

func (s *Server) runtime(id session.PaneID) (*paneRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	rt, ok := s.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	return rt, nil
}

// --- goroutines ------------------------------------------------------------

// readPane copies a pane's output into its terminal until the process ends.
func (s *Server) readPane(rt *paneRuntime) {
	defer s.wg.Done()

	buf := make([]byte, readBuffer)
	for {
		n, err := rt.pty.Read(buf)
		if n > 0 {
			rt.write(buf[:n])
			s.events.publish(Event{Kind: EventPaneOutput, Pane: rt.id})
		}
		if err != nil {
			break
		}
	}

	waitErr := rt.pty.Wait()
	if deliberate := rt.markExited(waitErr); deliberate {
		// tend hung this pane up itself. Reporting the resulting "hangup" as
		// the process failing would put an error on every pane the user closes.
		return
	}

	ev := Event{Kind: EventPaneExited, Pane: rt.id}
	if waitErr != nil {
		ev.Err = waitErr.Error()
	}
	s.events.publish(ev)
}

// detectLoop re-examines panes on a tick.
//
// One loop serves every pane rather than one goroutine each: detection is
// per-pane work on a shared schedule, and a goroutine and timer per pane would
// scale the cost of an idle workspace with its size.
func (s *Server) detectLoop() {
	defer s.wg.Done()

	t := time.NewTicker(s.cfg.DetectInterval)
	defer t.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.detectOnce()
		}
	}
}

func (s *Server) detectOnce() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	type pending struct {
		rt        *paneRuntime
		lastTitle string
	}
	work := make([]pending, 0, len(s.runtimes))
	for id, rt := range s.runtimes {
		work = append(work, pending{rt: rt, lastTitle: s.titles[id]})
	}
	s.mu.Unlock()

	for _, w := range work {
		// Each pane is examined under its own lock, so a slow one delays only
		// itself.
		obs := w.rt.poll(w.lastTitle)
		if !obs.titleChanged && !obs.stateChanged {
			continue
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		if _, live := s.runtimes[w.rt.id]; !live {
			// The pane was closed while we were looking at it.
			s.mu.Unlock()
			continue
		}
		if obs.titleChanged {
			s.titles[w.rt.id] = obs.title
			if p, ok := s.session.Pane(w.rt.id); ok {
				p.Title = obs.title
			}
		}
		if obs.stateChanged {
			_ = s.session.SetPaneState(w.rt.id, w.rt.agentID, obs.state)
		}
		s.mu.Unlock()

		if obs.stateChanged {
			s.events.publish(Event{
				Kind:  EventPaneState,
				Pane:  w.rt.id,
				State: obs.state,
				Rule:  obs.rule,
			})
		}
	}
}
