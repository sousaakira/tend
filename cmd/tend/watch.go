package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// runWatch runs one command on a pty and reports what its terminal says about
// it. This is the whole stack in one line: pty feeds the terminal core, the
// core feeds detection, detection reports state.
func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	agentName := fs.String("agent", "", "agent manifest to use (default: the command's name)")
	quiet := fs.Bool("q", false, "do not mirror the command's output")
	capturePath := fs.String("capture", "", "also write the raw output to this file")
	interval := fs.Duration("interval", 200*time.Millisecond, "how often to re-read the screen")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend watch [options] -- command [args...]\n\n"+
				"runs a command on a pseudo-terminal and reports its agent state as it\n"+
				"changes. state goes to stderr, so the command's own output can be piped.\n\n"+
				"examples:\n"+
				"  tend watch -- claude\n"+
				"  tend watch -agent codex -capture out.raw -- codex\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cmdArgs := fs.Args()
	if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
		cmdArgs = cmdArgs[1:]
	}
	if len(cmdArgs) == 0 {
		fs.Usage()
		return errors.New("no command given")
	}

	manifest, err := resolveAgent(*agentName, cmdArgs[0])
	if err != nil {
		return err
	}

	capture, err := openCapture(*capturePath)
	if err != nil {
		return err
	}
	if capture != nil {
		defer capture.Close()
	}

	return watch(cmdArgs, manifest, watchOptions{
		mirror:   !*quiet,
		capture:  capture,
		interval: *interval,
	})
}

// resolveAgent picks the manifest, falling back to the command's own name so
// that `tend watch -- claude` needs no flag.
func resolveAgent(name, command string) (*detect.Manifest, error) {
	catalog, err := detect.Bundled()
	if err != nil {
		return nil, err
	}
	if name != "" {
		m, ok := catalog.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("unknown agent %q; run \"tend agents\" to list them", name)
		}
		return m, nil
	}

	base := strings.TrimSuffix(filepath.Base(command), ".exe")
	m, ok := catalog.Lookup(base)
	if !ok {
		return nil, fmt.Errorf("no manifest matches command %q; pass -agent, or run \"tend agents\"", base)
	}
	return m, nil
}

// openCapture returns an io.WriteCloser rather than an *os.File on purpose.
// Returning a nil *os.File and assigning it to an interface field yields an
// interface that is not nil but holds a nil pointer, so a nil check passes and
// the first write fails with "invalid argument". Returning the interface type
// makes the empty case a genuine nil.
func openCapture(path string) (io.WriteCloser, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating capture: %w", err)
	}
	return f, nil
}

type watchOptions struct {
	mirror   bool
	capture  io.Writer
	interval time.Duration
}

// outputs lists where raw pty output goes. It never returns a nil writer: one
// would fail the whole copy on its first write, which reads as the process
// dying immediately rather than as a bad option.
func (o watchOptions) outputs(screen io.Writer) []io.Writer {
	dst := []io.Writer{screen}
	if o.mirror {
		dst = append(dst, os.Stdout)
	}
	if o.capture != nil {
		dst = append(dst, o.capture)
	}
	return dst
}

// watcher owns the terminal state for one watched command.
//
// The pty reader and the detection ticker run on different goroutines and a
// vt.Screen is not safe for concurrent use, so every touch goes through this
// mutex. It is held only for the write or the snapshot itself: this sits on
// the per-byte path.
type watcher struct {
	mu       sync.Mutex
	screen   *vt.Screen
	detector *agent.Detector
	dirty    bool
}

func (w *watcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.screen.Write(p)
	w.dirty = true
	return n, err
}

// poll runs detection if the screen changed since the last look. Parsing
// output is cheap; re-running every rule over it is not, so an unchanged
// screen costs nothing.
func (w *watcher) poll() (detect.Result, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.dirty {
		return detect.Result{}, false
	}
	w.dirty = false
	return w.detector.Update(w.screen)
}

func (w *watcher) resize(size pty.Size) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.screen.Resize(int(size.Cols), int(size.Rows))
}

func watch(cmdArgs []string, manifest *detect.Manifest, opts watchOptions) error {
	size := terminalSize()

	w := &watcher{
		screen:   vt.NewScreen(int(size.Cols), int(size.Rows), 2000),
		detector: agent.NewDetector(manifest),
	}

	p, err := pty.Start(cmdArgs[0], cmdArgs[1:], pty.Options{Size: size})
	if err != nil {
		return fmt.Errorf("starting %s: %w", cmdArgs[0], err)
	}
	defer p.Close()

	restore := enterRawMode()
	defer restore()

	name := filepath.Base(cmdArgs[0])
	fmt.Fprintf(os.Stderr, "%s watching %s as %q on a %s terminal\n",
		tag(), name, manifest.ID, size)

	stopResize := watchResize(p, w)
	defer stopResize()

	// Input goes straight through, so the agent sees keystrokes exactly as it
	// would in a normal terminal. The copy ends when the pty closes.
	go func() { _, _ = io.Copy(p, os.Stdin) }()

	done := make(chan struct{})
	go pollLoop(w, opts.interval, done)

	_, copyErr := io.Copy(io.MultiWriter(opts.outputs(w)...), p)
	close(done)

	waitErr := p.Wait()

	restore()
	resetTerminal()

	// One last look: the final frame often carries the state that matters,
	// such as an agent finishing just as it exits.
	if res, changed := w.poll(); changed {
		report(res)
	}
	fmt.Fprintf(os.Stderr, "%s %s exited (final state: %s)\n",
		tag(), name, w.detector.State())

	if waitErr != nil {
		return waitErr
	}
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return copyErr
	}
	return nil
}

func pollLoop(w *watcher, interval time.Duration, done <-chan struct{}) {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if res, changed := w.poll(); changed {
				report(res)
			}
		}
	}
}

func report(res detect.Result) {
	rule := res.RuleID
	if rule == "" {
		rule = "no rule matched"
	}
	fmt.Fprintf(os.Stderr, "%s %s%-8s%s  %s\n",
		tag(), stateColor(res.State), res.State, reset(), rule)
}

// --- terminal plumbing -----------------------------------------------------

func terminalSize() pty.Size {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return pty.DefaultSize
	}
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 || rows <= 0 {
		return pty.DefaultSize
	}
	return pty.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

// enterRawMode hands keystrokes to the child unmodified, which is what an
// agent's own key handling needs. The returned function restores the terminal
// and is safe to call more than once.
func enterRawMode() func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = term.Restore(fd, state) })
	}
}

// resetTerminal undoes what a full-screen agent may have left behind. Without
// it, quitting mid-draw can leave the shell on the alternate screen with the
// cursor hidden and mouse reporting still on.
func resetTerminal() {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	fmt.Fprint(os.Stdout, "\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h\x1b[0m")
}

// watchResize keeps the child's terminal the same size as ours, so a
// full-screen agent redraws when the window changes.
func watchResize(p *pty.Pty, w *watcher) func() {
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
				size := terminalSize()
				_ = p.Resize(size)
				w.resize(size)
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// --- output styling --------------------------------------------------------

func colorsEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

func tag() string {
	if colorsEnabled() {
		return "\x1b[2m[tend]\x1b[0m"
	}
	return "[tend]"
}

func stateColor(s detect.State) string {
	if !colorsEnabled() {
		return ""
	}
	switch s {
	case detect.StateWorking:
		return "\x1b[33m"
	case detect.StateBlocked:
		return "\x1b[31m"
	case detect.StateIdle:
		return "\x1b[32m"
	default:
		return "\x1b[2m"
	}
}

func reset() string {
	if colorsEnabled() {
		return "\x1b[0m"
	}
	return ""
}
