package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/vt"
)

// What a script sends to a pane is not what a person sends. A person's
// terminal has already encoded the keystroke and the client forwards the
// bytes; a script has a name, "enter" or "ctrl+c", and text it wants pasted.
// Encoding that needs the pane's own modes — whether the program asked for
// bracketed paste, whether its arrow keys are in application mode — so it
// happens here, beside the terminal that knows, rather than in the caller.

// ErrUnknownKey means a key name nothing could encode.
type ErrUnknownKey struct{ Key string }

func (e *ErrUnknownKey) Error() string { return fmt.Sprintf("server: unsupported key %q", e.Key) }

// SendText writes text to a pane as a paste.
func (s *Server) SendText(id session.PaneID, text string) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	return s.writeTo(rt, vt.EncodeText(text, rt.modes()))
}

// SendKeys writes the named keys to a pane, in order. A name nothing can
// encode stops the lot: half a key sequence is worse than none, since the
// program acts on what did arrive.
func (s *Server) SendKeys(id session.PaneID, keys []string) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	modes := rt.modes()
	var out []byte
	for _, k := range keys {
		b, ok := vt.EncodeKey(k, modes)
		if !ok {
			return &ErrUnknownKey{Key: k}
		}
		out = append(out, b...)
	}
	return s.writeTo(rt, out)
}

// SubmitDelay is how long the text of a prompt is given before the Enter that
// sends it.
//
// It is herdr's 300ms (`AGENT_PROMPT_SUBMIT_DELAY`), kept because herdr keeps
// it: an agent's prompt box reads its input in chunks, and one that takes the
// carriage return before the paste that preceded it sends an empty prompt.
// Claude Code v2.1.278 was tried both ways here and accepted both, so this is
// herdr's caution rather than a bug seen in tend. The pause is per prompt, and
// nothing else waits on it.
const SubmitDelay = 300 * time.Millisecond

// Submit is text followed by enter: a prompt handed to an agent.
func (s *Server) Submit(id session.PaneID, text string) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	modes := rt.modes()
	if err := s.writeTo(rt, vt.EncodeText(text, modes)); err != nil {
		return err
	}
	enter, _ := vt.EncodeKey("enter", modes)

	select {
	case <-time.After(SubmitDelay):
	case <-s.done:
		return ErrClosed // the server is stopping; do not send half a prompt
	}
	// Asked again: the pane may have gone in the meantime, and writing to a
	// terminal whose process has ended is an error worth reporting as one.
	if _, err := s.runtime(id); err != nil {
		return err
	}
	return s.writeTo(rt, enter)
}

// writeTo sends bytes to a pane, refusing to write nothing.
func (s *Server) writeTo(rt *paneRuntime, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	_, err := rt.pty.Write(b)
	return err
}

// modes is what the program in the pane has asked its terminal for.
func (rt *paneRuntime) modes() vt.Modes {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.screen.Modes()
}

// RecentText is the last lines a pane has shown, history included.
//
// What a script wants to read is not the rectangle a client draws: an agent's
// answer has scrolled off by the time anything asks, and the blank rows below
// a short answer are noise. This returns the tail of what was said, with the
// trailing blank lines dropped.
func (s *Server) RecentText(id session.PaneID, lines int) (string, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return "", err
	}
	var out string
	rt.withScreen(func(screen *vt.Screen) {
		out = recentText(screen, lines)
	})
	return out, nil
}

// recentText reads the tail of a screen. It is a function of the screen alone,
// which is what makes it testable without a process behind it.
func recentText(screen *vt.Screen, lines int) string {
	grid := screen.Grid()
	history := 0
	if grid == screen.MainGrid() {
		// Only the main screen keeps history. The alternate screen not having
		// one is the point of it: a full-screen program puts the terminal back
		// as it found it.
		history = grid.HistoryLen()
	}
	total := history + grid.Rows()

	rows := make([]string, 0, total)
	for i := 0; i < total; i++ {
		var row *vt.Row
		if i < history {
			row = grid.HistoryLine(i)
		} else {
			row = grid.Line(i - history)
		}
		rows = append(rows, strings.TrimRight(row.Text(), " \t"))
	}
	// Trailing blanks are the empty part of the screen, not something said.
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if lines > 0 && len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	return strings.Join(rows, "\n")
}

// VisibleText is what is on the pane's screen now, as detection sees it.
func (s *Server) VisibleText(id session.PaneID) (string, error) { return s.ScreenText(id) }

// AgentPanes lists the panes that are an agent, in session order.
func (s *Server) AgentPanes() []PaneStatus {
	var out []PaneStatus
	for _, st := range s.Statuses() {
		if st.Agent != "" {
			out = append(out, st)
		}
	}
	return out
}

// ResolvePaneAgent finds the pane a script named by agent: its label, or the
// name of the command it runs. Ambiguity is an error rather than a guess —
// two claudes and a prompt sent to the wrong one is not recoverable.
func (s *Server) ResolvePaneAgent(name string) (session.PaneID, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	var found []PaneStatus
	for _, st := range s.AgentPanes() {
		if strings.ToLower(st.Agent) == name {
			found = append(found, st)
		}
	}
	switch len(found) {
	case 0:
		return 0, fmt.Errorf("%w: no pane is running %q", session.ErrNoSuchPane, name)
	case 1:
		return found[0].ID, nil
	}
	ids := make([]string, 0, len(found))
	for _, st := range found {
		ids = append(ids, fmt.Sprintf("%d", st.ID))
	}
	return 0, fmt.Errorf("%d panes are running %q (%s): name the pane instead",
		len(found), name, strings.Join(ids, ", "))
}

// AgentSessionOf is the conversation known for a pane, or none.
func (s *Server) AgentSessionOf(id session.PaneID) (agent.PersistedSession, bool) {
	p, ok, err := s.AgentSession(id)
	if err != nil {
		return agent.PersistedSession{}, false
	}
	return p, ok
}
