package server

import (
	"errors"
	"strings"
	"sync"

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
	it.Source = "browser"
	if it.Kind == "" {
		it.Kind = "element"
	}
	return s.AddContext(it)
}

// BrowserSendToAgent captures an item and types it into a pane — the one
// named, or the one the user was last in — for the agent there, submitting
// nothing. It returns the pane it went to.
func (s *Server) BrowserSendToAgent(it proto.ContextItem, pane session.PaneID) (session.PaneID, error) {
	kept, err := s.BrowserCapture(it)
	if err != nil {
		return 0, err
	}
	if pane == 0 {
		pane = s.FocusedPane()
	}
	if pane == 0 {
		return 0, errors.New("browser: no pane to send to; name one")
	}
	return pane, s.SendContext(pane, []uint64{kept.ID})
}
