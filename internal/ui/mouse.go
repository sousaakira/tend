package ui

import (
	"strconv"
	"strings"
)

// Mouse reporting is read in the SGR encoding, which tend asks the terminal
// for. The older encodings pack coordinates into single bytes and cannot
// describe a click past column 223, which any real terminal exceeds.
//
// A sequence looks like ESC [ < button ; column ; row M for a press and the
// same with a trailing m for a release, with columns and rows counted from 1.

// MouseKind is what a mouse report describes.
type MouseKind uint8

const (
	MouseNone MouseKind = iota
	MousePress
	MouseRelease
	MouseDrag
	MouseWheelUp
	MouseWheelDown
)

// MouseEvent is one report, in cells counted from zero.
type MouseEvent struct {
	Kind   MouseKind
	X, Y   int
	Button int
	// Raw is the sequence exactly as it arrived, for forwarding to a pane
	// whose own program asked for the mouse.
	Raw []byte
}

// EnableMouse is what a client sends to ask for mouse reporting: button
// events including drags, in the SGR encoding.
const EnableMouse = "\x1b[?1002h\x1b[?1006h"

// DisableMouse undoes it. Leaving a terminal in mouse-reporting mode makes
// every later click in that window emit gibberish, so it is always undone.
const DisableMouse = "\x1b[?1002l\x1b[?1006l"

// parseMouse reads an SGR mouse report from the start of data.
//
// The three outcomes are distinct and all matter: a report was read, the data
// is the beginning of one that has not fully arrived, or it is not a mouse
// report at all. Treating the second as the third would forward half a
// sequence to a pane and leave the rest to appear as typed text.
func parseMouse(data []byte) (ev MouseEvent, n int, incomplete bool) {
	const prefix = "\x1b[<"
	if len(data) < len(prefix) {
		return MouseEvent{}, 0, strings.HasPrefix(prefix, string(data))
	}
	if !strings.HasPrefix(string(data), prefix) {
		return MouseEvent{}, 0, false
	}

	end := -1
	for i := len(prefix); i < len(data); i++ {
		if data[i] == 'M' || data[i] == 'm' {
			end = i
			break
		}
		if data[i] != ';' && (data[i] < '0' || data[i] > '9') {
			return MouseEvent{}, 0, false // not a mouse report after all
		}
	}
	if end < 0 {
		return MouseEvent{}, 0, true
	}

	fields := strings.Split(string(data[len(prefix):end]), ";")
	if len(fields) != 3 {
		return MouseEvent{}, 0, false
	}
	button, err1 := strconv.Atoi(fields[0])
	col, err2 := strconv.Atoi(fields[1])
	row, err3 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return MouseEvent{}, 0, false
	}

	ev = MouseEvent{
		X:      col - 1, // reports count from one
		Y:      row - 1,
		Button: button & 3,
		Raw:    data[:end+1],
	}
	switch {
	case button&64 != 0: // the wheel reports as buttons 64 and 65
		if button&1 == 0 {
			ev.Kind = MouseWheelUp
		} else {
			ev.Kind = MouseWheelDown
		}
	case button&32 != 0: // motion with a button held
		ev.Kind = MouseDrag
	case data[end] == 'm':
		ev.Kind = MouseRelease
	default:
		ev.Kind = MousePress
	}
	return ev, end + 1, false
}
