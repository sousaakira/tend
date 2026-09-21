package server

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/proto"
)

// The right end of the tab bar: herdr's `app/tab_bar_status.rs`.
//
// The server works these out, not the client, for the reason herdr gives:
// the clock and the commands that mean something are the ones on the machine
// the panes are on, and a client attached from a laptop would otherwise show
// the laptop's. What each client does with them — whether ZOOM applies, since
// zoom is one person's view — it decides for itself.
//
// A command runs again only when its last run is over, never two at once,
// and is killed with everything it started when it overruns or the bar is
// reconfigured: a status line that forks a process every few seconds for the
// life of a session must not be able to pile them up.

// Limits, herdr's.
const (
	maxStatusText      = 80
	maxStatusLineBytes = 4096
	datetimeRefresh    = time.Second
)

// tabBar is the configured entries and what each one says now.
type tabBar struct {
	mu        sync.Mutex
	entries   []config.TabBarEntry
	separator string
	// values holds each entry's current text, index for index. Empty hides
	// the entry, as herdr hides a command that printed nothing.
	values []string
	// generation increases with every reconfiguration, so a command that
	// finishes after its entry was replaced cannot write into the new one.
	generation uint64
	cancel     context.CancelFunc
}

// ConfigureTabBar replaces what the bar shows and starts whatever keeps it
// current. Called at start and on every reload.
func (s *Server) ConfigureTabBar(entries []config.TabBarEntry, separator string) {
	if len(entries) > config.MaxTabBarEntries {
		entries = entries[:config.MaxTabBarEntries]
	}
	bar := &s.tabBar
	bar.mu.Lock()
	if bar.cancel != nil {
		bar.cancel()
	}
	bar.generation++
	gen := bar.generation
	bar.entries = append([]config.TabBarEntry(nil), entries...)
	bar.separator = strings.Map(dropControl, separator)
	bar.values = make([]string, len(entries))
	ctx, cancel := context.WithCancel(s.context())
	bar.cancel = cancel

	hasClock := false
	for i, e := range entries {
		switch e.Type {
		case "hostname":
			bar.values[i] = statusText(hostname())
		case "text":
			bar.values[i] = strings.Map(dropControl, e.Text)
		case "datetime":
			hasClock = true
			bar.values[i] = statusText(config.Strftime(e.DatetimeFormat(), time.Now()))
		case "command":
			s.wg.Add(1)
			go func(i int, e config.TabBarEntry) {
				defer s.wg.Done()
				s.runStatusCommand(ctx, gen, i, e)
			}(i, e)
		}
	}
	bar.mu.Unlock()

	if hasClock {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runStatusClock(ctx, gen)
		}()
	}
	s.publish(Event{Kind: EventSessionChanged})
}

// tabBarDiffers reports whether a configuration would change the bar.
func (s *Server) tabBarDiffers(entries []config.TabBarEntry, separator string) bool {
	bar := &s.tabBar
	bar.mu.Lock()
	defer bar.mu.Unlock()
	if len(entries) > config.MaxTabBarEntries {
		entries = entries[:config.MaxTabBarEntries]
	}
	if strings.Map(dropControl, separator) != bar.separator || len(entries) != len(bar.entries) {
		return true
	}
	for i := range entries {
		if entries[i] != bar.entries[i] {
			return true
		}
	}
	return false
}

// tabBarSnapshot is what goes out to clients.
func (s *Server) tabBarSnapshot() ([]proto.StatusSegment, string) {
	bar := &s.tabBar
	bar.mu.Lock()
	defer bar.mu.Unlock()
	var out []proto.StatusSegment
	for i, e := range bar.entries {
		if e.Type == "zoom" {
			out = append(out, proto.StatusSegment{Zoom: true})
			continue
		}
		if bar.values[i] != "" {
			out = append(out, proto.StatusSegment{Text: bar.values[i]})
		}
	}
	return out, bar.separator
}

// setStatus records an entry's new text and tells clients when it changed.
func (s *Server) setStatus(gen uint64, index int, text string) {
	bar := &s.tabBar
	bar.mu.Lock()
	if gen != bar.generation || index >= len(bar.values) || bar.values[index] == text {
		bar.mu.Unlock()
		return
	}
	bar.values[index] = text
	bar.mu.Unlock()
	s.publish(Event{Kind: EventSessionChanged})
}

// runStatusClock rewrites the datetime entries every second. Clients hear of
// it only when the text changes, which for "%H:%M" is once a minute.
func (s *Server) runStatusClock(ctx context.Context, gen uint64) {
	ticker := time.NewTicker(datetimeRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.tabBar.mu.Lock()
			entries := s.tabBar.entries
			s.tabBar.mu.Unlock()
			for i, e := range entries {
				if e.Type == "datetime" {
					s.setStatus(gen, i, statusText(config.Strftime(e.DatetimeFormat(), now)))
				}
			}
		}
	}
}

// runStatusCommand runs one command entry on its interval until the bar is
// reconfigured or the server stops.
func (s *Server) runStatusCommand(ctx context.Context, gen uint64, index int, e config.TabBarEntry) {
	for {
		started := time.Now()
		text, err := s.statusCommandOnce(ctx, e)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.logf("tab bar command %q: %v", e.Command, err)
		}
		// A failed run hides the entry, as herdr does: the last good value
		// would claim to be current when it is not.
		s.setStatus(gen, index, text)

		wait := time.Until(started.Add(e.Interval()))
		if wait <= 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// statusCommandOnce runs the command and returns the last line it printed.
func (s *Server) statusCommandOnce(ctx context.Context, e config.TabBarEntry) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", e.Command)
	env, dir := s.statusCommandEnv()
	cmd.Env = env
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &limitedLastLine{buf: &out}
	cmd.Stdin, cmd.Stderr = nil, nil
	setCommandGroup(cmd)
	cmd.Cancel = func() error { return killCommandGroup(cmd) }
	// Stdout is a pipe exec reads; a grandchild holding it would keep the
	// wait going after the command is dead. The plugin host learned this.
	cmd.WaitDelay = 500 * time.Millisecond

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errTimedOut(e.Timeout())
		}
		return "", err
	}
	return statusText(string(stripControlSequences(lastLine(out.Bytes())))), nil
}

// statusCommandEnv is what a command is told: the socket to call back on and
// what the user is looking at, as herdr's `custom_command_env` does, and it
// runs in the focused pane's directory.
func (s *Server) statusCommandEnv() ([]string, string) {
	env := append(os.Environ(), s.cfg.CommandEnv...)
	s.mu.Lock()
	pane, tab, ws := s.focusedPane, s.focusedTab, s.focusedWorkspace
	dir := ""
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if p, ok := t.Pane(pane); ok {
				dir = p.Dir
			}
		}
	}
	s.mu.Unlock()
	if ws != 0 {
		env = append(env, "TEND_ACTIVE_WORKSPACE_ID=w_"+itoa(uint64(ws)))
	}
	if tab != 0 {
		env = append(env, "TEND_ACTIVE_TAB_ID=t_"+itoa(uint64(tab)))
	}
	if pane != 0 {
		env = append(env, "TEND_ACTIVE_PANE_ID=p_"+itoa(uint64(pane)))
	}
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			dir = ""
		}
	}
	return env, dir
}

type timeoutError time.Duration

func (e timeoutError) Error() string {
	return "timed out after " + time.Duration(e).String()
}

func errTimedOut(d time.Duration) error { return timeoutError(d) }

// limitedLastLine keeps what a command printed, but never more than one long
// line's worth: a command that prints forever must not grow the server.
type limitedLastLine struct {
	buf *bytes.Buffer
}

func (l *limitedLastLine) Write(p []byte) (int, error) {
	n := len(p)
	l.buf.Write(p)
	if l.buf.Len() > 2*maxStatusLineBytes {
		// Keep the tail: the last line is what is shown.
		tail := append([]byte(nil), l.buf.Bytes()[l.buf.Len()-maxStatusLineBytes:]...)
		l.buf.Reset()
		l.buf.Write(tail)
	}
	return n, nil
}

// lastLine is the last line of output, with or without a newline after it.
func lastLine(out []byte) []byte {
	out = bytes.TrimSuffix(out, []byte("\n"))
	if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
		out = out[i+1:]
	}
	if len(out) > maxStatusLineBytes {
		out = out[:maxStatusLineBytes]
	}
	return out
}

// statusText is what may be shown: trimmed, no control or invisible format
// characters, at most herdr's 80 characters.
func statusText(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if n == maxStatusText {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func dropControl(r rune) rune {
	if unicode.IsControl(r) {
		return -1
	}
	return r
}

// stripControlSequences removes escape sequences from a command's output, by
// herdr's state machine: a colourised status line is still a status line, and
// its escapes must not reach the bar as text or as commands.
func stripControlSequences(in []byte) []byte {
	const (
		text = iota
		escape
		intermediate
		csi
		osc
		str
	)
	out := make([]byte, 0, len(in))
	state := text
	for _, c := range in {
		// cancel ends any sequence; ctl is any other C0 control or DEL,
		// which a sequence carries on through.
		cancel := c == 0x18 || c == 0x1a
		ctl := (c < 0x20 || c == 0x7f) && c != 0x1b && !cancel
		switch state {
		case text:
			if c == 0x1b {
				state = escape
			} else {
				out = append(out, c)
			}
		case escape:
			switch {
			case c == '[':
				state = csi
			case c == ']':
				state = osc
			case c == 'P' || c == 'X' || c == '^' || c == '_':
				state = str
			case c >= 0x20 && c <= 0x2f:
				state = intermediate
			case c >= 0x30 && c <= 0x7e, cancel:
				state = text
			case c == 0x1b, ctl:
			default:
				out = append(out, c)
				state = text
			}
		case intermediate:
			switch {
			case c >= 0x20 && c <= 0x2f, ctl:
			case c >= 0x30 && c <= 0x7e, cancel:
				state = text
			case c == 0x1b:
				state = escape
			default:
				out = append(out, c)
				state = text
			}
		case csi:
			switch {
			case c >= 0x20 && c <= 0x3f, ctl:
			case c >= 0x40 && c <= 0x7e, cancel:
				state = text
			case c == 0x1b:
				state = escape
			default:
				out = append(out, c)
				state = text
			}
		case osc:
			switch c {
			case 0x07, 0x18, 0x1a:
				state = text
			case 0x1b:
				state = escape
			}
		case str:
			switch c {
			case 0x18, 0x1a:
				state = text
			case 0x1b:
				state = escape
			}
		}
	}
	return out
}
