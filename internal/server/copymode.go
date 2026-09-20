package server

import (
	"errors"

	"github.com/sousaakira/tend/internal/copymode"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
	"github.com/sousaakira/tend/internal/vt"
)

// ErrUnknownMotion means a motion copy mode does not have.
var ErrUnknownMotion = errors.New("server: unknown copy-mode motion")

var motions = map[string]bool{
	copymode.NextWordStart: true, copymode.PreviousWordStart: true, copymode.NextWordEnd: true,
	copymode.NextBigWordStart: true, copymode.PreviousBigWordStart: true, copymode.NextBigWordEnd: true,
	copymode.LineEnd: true, copymode.FirstNonBlank: true,
	copymode.PreviousParagraph: true, copymode.NextParagraph: true,
}

// CopyMotion answers where a copy-mode motion from a point lands.
//
// Here and not in the client because the text is here. The client would have
// to fetch the pane's whole history to find the next word two lines down, and
// that is most of what a keypress in copy mode asks.
func (s *Server) CopyMotion(id session.PaneID, from proto.CopyPoint, motion string) (proto.PaneCopyResult, error) {
	if !motions[motion] {
		return proto.PaneCopyResult{}, ErrUnknownMotion
	}
	rt, err := s.runtime(id)
	if err != nil {
		return proto.PaneCopyResult{}, err
	}
	var out proto.PaneCopyResult
	rt.withScreen(func(screen *vt.Screen) {
		text := copymode.FromScreen(screen)
		to := copymode.Move(text, copymode.Point{Row: from.Row, Col: from.Col}, motion)
		out = proto.PaneCopyResult{
			To:      proto.CopyPoint{Row: to.Row, Col: to.Col},
			Found:   true,
			History: text.History(),
			Rows:    text.Rows() - text.History(),
		}
	})
	return out, nil
}

// CopySearch answers where the nearest match of a query is.
func (s *Server) CopySearch(id session.PaneID, from proto.CopyPoint, query, direction string) (proto.PaneCopyResult, error) {
	if direction != copymode.Backward {
		direction = copymode.Forward
	}
	rt, err := s.runtime(id)
	if err != nil {
		return proto.PaneCopyResult{}, err
	}
	var out proto.PaneCopyResult
	rt.withScreen(func(screen *vt.Screen) {
		text := copymode.FromScreen(screen)
		out.History = text.History()
		out.Rows = text.Rows() - text.History()
		m, total, ok := copymode.Search(text, query, copymode.Point{Row: from.Row, Col: from.Col}, direction)
		if !ok {
			out.To = from
			return
		}
		out.To = proto.CopyPoint{Row: m.Start.Row, Col: m.Start.Col}
		out.End = proto.CopyPoint{Row: m.End.Row, Col: m.End.Col}
		out.Found, out.Total = true, total
	})
	return out, nil
}
