package ui

import (
	"strconv"
	"strings"
)

// PopupRect is where a popup goes in an area, herdr's
// resolve_popup_geometry: a width and a height each a number of cells or a
// percentage of the area, half of it when not given, at least 6 by 4, at
// most the area, centred.
func PopupRect(area Rect, width, height string) (Rect, bool) {
	w := resolvePopupSize(width, area.Cols, max(area.Cols/2, 6))
	h := resolvePopupSize(height, area.Rows, max(area.Rows/2, 4))
	w, h = min(max(w, 6), area.Cols), min(max(h, 4), area.Rows)
	if w < 6 || h < 4 {
		return Rect{}, false
	}
	return Rect{X: area.X + (area.Cols-w)/2, Y: area.Y + (area.Rows-h)/2, Cols: w, Rows: h}, true
}

func resolvePopupSize(v string, available, fallback int) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	if pct, ok := strings.CutSuffix(v, "%"); ok {
		if n, err := strconv.Atoi(pct); err == nil && n >= 1 && n <= 100 {
			return available * n / 100
		}
		return fallback
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return fallback
}
