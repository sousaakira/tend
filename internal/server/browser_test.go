package server

import (
	"errors"
	"testing"

	"github.com/sousaakira/tend/internal/proto"
)

// TestCommandsGoToTheBrowsersAttached: a command reaches every browser
// attached and says how many; with none it is ErrNoBrowser, so the client
// opens the page itself; a page that is not http(s) is refused; a browser
// let go gets nothing more; what a browser captures is the browser's in the
// context. If it regresses, a page asked for goes nowhere and nobody is
// told, or a browser is sent a file: URL.
func TestCommandsGoToTheBrowsersAttached(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.SendBrowser(BrowserCommand{Action: BrowserOpen, URL: "https://example.com"}); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("none attached: %v", err)
	}
	cmds, detach := s.AttachBrowser("test")
	if names := s.Browsers(); len(names) != 1 || names[0] != "test" {
		t.Errorf("attached: %v", names)
	}
	if n, err := s.SendBrowser(BrowserCommand{Action: BrowserNavigate, URL: "https://example.com/login"}); err != nil || n != 1 {
		t.Fatalf("sent to %d: %v", n, err)
	}
	if got := <-cmds; got.Action != BrowserNavigate || got.URL != "https://example.com/login" {
		t.Errorf("received %+v", got)
	}
	if _, err := s.SendBrowser(BrowserCommand{Action: BrowserOpen, URL: "file:///etc/passwd"}); err == nil {
		t.Error("a file: URL is refused")
	}
	detach()
	if _, err := s.SendBrowser(BrowserCommand{Action: BrowserSelect, On: true}); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("let go: %v", err)
	}

	it, err := s.BrowserCapture(proto.ContextItem{URL: "https://example.com/login", Selector: "#email"})
	if err != nil || it.Kind != "element" || it.Source != "browser" {
		t.Errorf("captured: %+v %v", it, err)
	}
	if _, err := s.BrowserSendToAgent(proto.ContextItem{Selector: "#go"}, 0); err == nil {
		t.Error("no pane in focus, none named: an error")
	}
}
