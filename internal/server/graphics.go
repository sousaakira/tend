package server

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// A pane's images live in its terminal, which is here; the terminal that can
// draw them is the client's. So the server hands them over and the client puts
// them on its own screen, which is what herdr does too — its images come out
// of the pane and go back in as host placements.
//
// The whole image is sent, once per revision rather than per redraw: a client
// asks only when the revision it holds is out of date, and the revision moves
// when an image or a placement does.

// PaneGraphics is a pane's images and where they go.
func (s *Server) PaneGraphics(id session.PaneID) (proto.PaneGraphicsResult, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return proto.PaneGraphicsResult{}, err
	}

	var out proto.PaneGraphicsResult
	rt.withScreen(func(screen *vt.Screen) {
		out.Revision = screen.KittyRevision()
		if grid := screen.Grid(); grid == screen.MainGrid() {
			out.History = grid.HistoryLen()
		}
		for _, img := range screen.KittyImages() {
			out.Images = append(out.Images, proto.GraphicsImage{
				ID: img.ID, Format: uint8(img.Format),
				Width: img.Width, Height: img.Height, Data: img.Data,
			})
		}
		for _, p := range screen.KittyPlacements() {
			out.Placements = append(out.Placements, proto.GraphicsPlacement{
				ImageID: p.ImageID, ID: p.ID, Row: p.Row, Col: p.Col,
				Cols: p.Cols, Rows: p.Rows, Z: p.Z,
			})
		}
	})
	return out, nil
}
