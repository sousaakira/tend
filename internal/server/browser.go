package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/sousaakira/tend/internal/capture"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
)

// Browsers are tend's own, and this is their foundation: the server keeps
// the browsers attached to it — a real browser, through the extension that
// is to come and the bridge it talks to the socket with — sends them what to
// do (open a page, go to one, turn element picking on or off), and takes
// what they capture into the context buffer (context.go), from where it
// reaches an agent like anything else captured. No browser talks to an agent
// directly, and nothing here renders a page: that is the browser's.
//
// The automation socket is the door (internal/api: browser.attach turns a
// connection into the stream of commands a browser follows; browser.open,
// .navigate, .select, .context and .send_to_agent are the rest).

// Browser actions a command carries.
const (
	BrowserOpen     = "open"
	BrowserNavigate = "navigate"
	BrowserSelect   = "select"
)

// BrowserCommand is one thing an attached browser is told to do: open a
// page in a new tab, go to one in the tab in front, or turn picking an
// element on or off.
type BrowserCommand struct {
	Action string
	URL    string
	On     bool
}

// ErrNoBrowser is a command with no browser attached to follow it.
var ErrNoBrowser = errors.New("no browser is attached to this session")

// browserBuffer is how many commands wait for a browser slow to read them;
// past it the newest are dropped for that browser, which is told nothing
// worse than a page it was not asked to open.
const browserBuffer = 16

// browserHub is the browsers attached, under its own lock: a command is
// handed out without the server's.
type browserHub struct {
	mu    sync.Mutex
	next  int
	conns map[int]attachedBrowser
}

type attachedBrowser struct {
	name string
	ch   chan BrowserCommand
}

// AttachBrowser registers a browser, and returns the commands it is to
// follow and the way to let it go. name says which browser it is, for
// browser.status.
func (s *Server) AttachBrowser(name string) (<-chan BrowserCommand, func()) {
	h := &s.browsers
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns == nil {
		h.conns = map[int]attachedBrowser{}
	}
	h.next++
	id := h.next
	ch := make(chan BrowserCommand, browserBuffer)
	h.conns[id] = attachedBrowser{name: strings.TrimSpace(name), ch: ch}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.conns, id)
			h.mu.Unlock()
		})
	}
}

// Browsers names the browsers attached.
func (s *Server) Browsers() []string {
	h := &s.browsers
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for _, b := range h.conns {
		name := b.name
		if name == "" {
			name = "browser"
		}
		out = append(out, name)
	}
	return out
}

// SendBrowser hands a command to every browser attached and says how many
// took it; with none attached it is ErrNoBrowser, so a caller can do
// something else — the client opens the page itself.
func (s *Server) SendBrowser(cmd BrowserCommand) (int, error) {
	switch cmd.Action {
	case BrowserOpen, BrowserNavigate:
		if !webURL(cmd.URL) {
			return 0, errors.New("browser: only http and https pages")
		}
	case BrowserSelect:
	default:
		return 0, errors.New("browser: action is open, navigate or select")
	}
	h := &s.browsers
	h.mu.Lock()
	defer h.mu.Unlock()
	sent := 0
	for _, b := range h.conns {
		select {
		case b.ch <- cmd:
			sent++
		default:
		}
	}
	if sent == 0 {
		return 0, ErrNoBrowser
	}
	return sent, nil
}

// webURL is herdr's safe_web_url: a page, not a file or a script.
func webURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// BrowserCapture takes what a browser picked into the context buffer, as
// the browser's.
func (s *Server) BrowserCapture(it proto.ContextItem) (proto.ContextItem, error) {
	kept, err := s.BrowserCaptureAll([]proto.ContextItem{it}, "")
	if err != nil {
		return proto.ContextItem{}, err
	}
	return kept[0], nil
}

// BrowserCaptureAll takes what a browser picked into the context buffer —
// several elements with their notes, and the message the user wrote for them
// all, kept first as text — and tells the clients it has arrived, so the one
// the user is at shows it: the context panel is where it is looked over and
// sent on. An item unfit for the buffer stops the lot before any is kept.
func (s *Server) BrowserCaptureAll(items []proto.ContextItem, message string) ([]proto.ContextItem, error) {
	var all []proto.ContextItem
	if message = strings.TrimSpace(message); message != "" {
		all = append(all, proto.ContextItem{Kind: capture.KindText, Text: message})
	}
	all = append(all, items...)
	if len(all) == 0 {
		return nil, errors.New("browser: nothing to capture")
	}
	for i := range all {
		all[i].Source = "browser"
		if all[i].Kind == "" {
			all[i].Kind = capture.KindElement
		}
		if err := capture.Check(all[i]); err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}
	}
	kept := make([]proto.ContextItem, 0, len(all))
	for _, it := range all {
		k, err := s.AddContext(it)
		if err != nil {
			return kept, err
		}
		kept = append(kept, k)
	}
	s.publish(Event{Kind: EventContextArrived, Title: "browser", Body: strconv.Itoa(len(kept))})
	return kept, nil
}

// BrowserSendToAgent captures items and types them, together and led by the
// user's message for them, into a pane —
// the one named, or the one the user was last in — for the agent there,
// submitting nothing: the elements picked on a page and the notes on them,
// as one message. It returns the pane they went to. An item that is not fit
// for the buffer stops the lot before any is kept.
func (s *Server) BrowserSendToAgent(items []proto.ContextItem, pane session.PaneID, message string) (session.PaneID, error) {
	if len(items) == 0 {
		return 0, errors.New("browser: nothing to send")
	}
	if pane == 0 {
		pane = s.FocusedPane()
	}
	if pane == 0 {
		return 0, errors.New("browser: no pane to send to; name one")
	}
	for i := range items {
		items[i].Source = "browser"
		if items[i].Kind == "" {
			items[i].Kind = "element"
		}
		if err := capture.Check(items[i]); err != nil {
			return 0, fmt.Errorf("item %d: %w", i+1, err)
		}
	}
	ids := make([]uint64, 0, len(items))
	for _, it := range items {
		kept, err := s.AddContext(it)
		if err != nil {
			return 0, err
		}
		ids = append(ids, kept.ID)
	}
	return pane, s.SendContextWith(pane, ids, message)
}
