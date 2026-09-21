package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sousaakira/tend/internal/session"
)

// A popup is herdr's (`app/popup.rs`, `popup_size.rs`): a terminal floating
// over the tab it was opened from, centred, in no layout — opening one moves
// no pane — with the keyboard while it is up. One at a time. It goes when
// its program ends, when it is closed (popup.close), or when its tab does.
// A user's command of type popup and a plugin's pane placed as a popup open
// one.

// ErrPopupOpen is refusing a second popup, as herdr refuses one.
var ErrPopupOpen = errors.New("server: a popup is already open")

// ErrNoPopup is closing a popup when none is open.
var ErrNoPopup = errors.New("server: no popup is open")

type popupState struct {
	pane          session.PaneID
	tab           session.TabID
	width, height string
	title         string
}

// Popup is the popup open, as a client is told of it.
type Popup struct {
	Pane          session.PaneID
	Tab           session.TabID
	Width, Height string
	Title         string
}

// ParsePopupSize reads herdr's popup size: a number of cells, or a
// percentage from 1% to 100%. Empty is the default, half the area.
func ParsePopupSize(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if pct, ok := strings.CutSuffix(value, "%"); ok {
		n, err := strconv.Atoi(pct)
		if err != nil || n < 1 || n > 100 {
			return fmt.Errorf("popup size %q: a percentage is 1%% to 100%%", value)
		}
		return nil
	}
	if n, err := strconv.Atoi(value); err != nil || n < 1 {
		return fmt.Errorf("popup size %q: a number of cells or a percentage like 80%%", value)
	}
	return nil
}

// OpenPopup starts a program in a popup over from's tab. Its directory is
// from's, as it is now, when none is given.
func (s *Server) OpenPopup(from session.PaneID, spec PaneSpec, width, height string) (session.PaneID, error) {
	for _, v := range []string{width, height} {
		if err := ParsePopupSize(v); err != nil {
			return 0, err
		}
	}
	if spec.Dir == "" {
		_, spec.Dir = s.commandEnv(from)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	if s.popup != nil {
		return 0, ErrPopupOpen
	}
	tab, ok := s.session.TabOf(from)
	if !ok {
		return 0, fmt.Errorf("%w: %d", session.ErrNoSuchPane, from)
	}
	id := s.session.AllocPaneID()
	spec.CloseOnExit, spec.NoDetect = true, true
	if err := s.startLocked(id, spec); err != nil {
		return 0, err
	}
	s.popup = &popupState{pane: id, tab: tab.ID, width: width, height: height, title: spec.Title}
	go s.publish(Event{Kind: EventSessionChanged})
	return id, nil
}

// FocusedPane is the pane a client last said it was on, or zero.
func (s *Server) FocusedPane() session.PaneID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.focusedPane
}

// PopupOpen is the popup open, if one is.
func (s *Server) PopupOpen() (Popup, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.popupLocked()
}

func (s *Server) popupLocked() (Popup, bool) {
	p := s.popup
	if p == nil {
		return Popup{}, false
	}
	return Popup{Pane: p.pane, Tab: p.tab, Width: p.width, Height: p.height, Title: p.title}, true
}

// ClosePopup closes the popup open, herdr's popup.close.
func (s *Server) ClosePopup() error {
	s.mu.Lock()
	p := s.popup
	s.mu.Unlock()
	if p == nil || !s.closePopupIf(p.pane) {
		return ErrNoPopup
	}
	return nil
}

// closePopupIf closes the popup when id is its pane, and reports whether
// it was: ClosePane goes through it, so a popup whose program ended closes
// by the same path a pane does.
func (s *Server) closePopupIf(id session.PaneID) bool {
	s.mu.Lock()
	if s.popup == nil || s.popup.pane != id {
		s.mu.Unlock()
		return false
	}
	s.popup = nil
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	delete(s.titles, id)
	s.mu.Unlock()
	if rt != nil {
		rt.setClosing()
		_ = rt.pty.Close()
	}
	s.publish(Event{Kind: EventPaneClosed, Pane: id})
	s.publish(Event{Kind: EventSessionChanged})
	return true
}

// reconcilePopup closes a popup whose tab has gone, herdr's rule: it
// belonged to that tab.
func (s *Server) reconcilePopup() {
	s.mu.Lock()
	p := s.popup
	gone := p != nil
	if p != nil {
		_, found := s.session.Tab(p.tab)
		gone = !found
	}
	s.mu.Unlock()
	if gone {
		s.closePopupIf(p.pane)
	}
}
