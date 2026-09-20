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
	// MouseMove is the pointer moving with no button held. It arrives only
	// while motion reporting is on, which costs a report per cell crossed and
	// so is asked for only while something is tracking the pointer.
	MouseMove
	MouseWheelUp
	MouseWheelDown
)

// MouseMod is a modifier key held during a mouse report.
//
// The terminal packs these into the button number rather than reporting them
// separately, which is why they are read here and not in the key decoder.
type MouseMod uint8

const (
	// ModShift, ModAlt and ModCtrl are the three the SGR encoding carries.
	ModShift MouseMod = 1 << iota
	ModAlt
	ModCtrl
)

// Has reports whether every modifier in m is held.
func (m MouseMod) Has(want MouseMod) bool { return m&want == want }

// MouseEvent is one report, in cells counted from zero.
type MouseEvent struct {
	Kind   MouseKind
	X, Y   int
	Button int
	// Mods are the modifier keys held, which decide whether a drag belongs to
	// the pane's own program or to the selection.
	Mods MouseMod
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

// EnableMotion asks for a report every time the pointer moves, not only while
// a button is held. DisableMotion goes back to the quieter mode.
//
// It is asked for only while something is following the pointer, because it
// costs a report per cell crossed: a pointer swept across a wide terminal is
// two hundred messages, parsed and acted on, for a menu that may not even be
// open.
const (
	EnableMotion  = "\x1b[?1003h"
	DisableMotion = "\x1b[?1003l\x1b[?1002h"
)

// mouseMods reads the modifier bits out of a button number.
func mouseMods(button int) MouseMod {
	var mods MouseMod
	if button&4 != 0 {
		mods |= ModShift
	}
	if button&8 != 0 {
		mods |= ModAlt
	}
	if button&16 != 0 {
		mods |= ModCtrl
	}
	return mods
}

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
		Mods:   mouseMods(button),
		Raw:    data[:end+1],
	}
	switch {
	case button&64 != 0: // the wheel reports as buttons 64 and 65
		if button&1 == 0 {
			ev.Kind = MouseWheelUp
		} else {
			ev.Kind = MouseWheelDown
		}
	case button&32 != 0 && button&3 == 3:
		// Motion with no button held. The encoding says "no button" the same
		// way it says "button 4", which is why this is checked before the
		// drag it would otherwise look like.
		ev.Kind = MouseMove
	case button&32 != 0: // motion with a button held
		ev.Kind = MouseDrag
	case data[end] == 'm':
		ev.Kind = MouseRelease
	default:
		ev.Kind = MousePress
	}
	return ev, end + 1, false
}

// EncodeMouse writes a mouse report for a pane's own program, at a position
// inside that pane counted from zero.
//
// The report that arrived cannot simply be passed on. Its coordinates are
// places on the whole screen, and the program believes it has a terminal to
// itself whose top-left cell is 1,1: handed the raw report, it sees every
// click displaced by the sidebar's width and the pane's border, and acts on
// text the pointer was never over.
//
// sgr says which encoding the program asked for. The older one packs each
// coordinate into a byte and cannot say anything past column 223, so a report
// out there is dropped rather than sent somewhere else.
func EncodeMouse(ev MouseEvent, x, y int, sgr bool) []byte {
	if x < 0 || y < 0 {
		return nil
	}
	code := ev.Button & 3
	switch ev.Kind {
	case MousePress, MouseRelease:
	case MouseDrag:
		code |= 32
	case MouseMove:
		code = 3 | 32
	case MouseWheelUp:
		code = 64
	case MouseWheelDown:
		code = 65
	default:
		return nil
	}
	if ev.Mods.Has(ModShift) {
		code |= 4
	}
	if ev.Mods.Has(ModAlt) {
		code |= 8
	}
	if ev.Mods.Has(ModCtrl) {
		code |= 16
	}

	if sgr {
		final := "M"
		if ev.Kind == MouseRelease {
			final = "m"
		}
		return []byte("\x1b[<" + strconv.Itoa(code) + ";" + strconv.Itoa(x+1) + ";" + strconv.Itoa(y+1) + final)
	}

	// The legacy encoding has no release per button: it says "some button
	// came up" as button 3.
	if ev.Kind == MouseRelease {
		code = code&^3 | 3
	}
	if x+1 > 223 || y+1 > 223 {
		return nil
	}
	return []byte{0x1b, '[', 'M', byte(32 + code), byte(32 + x + 1), byte(32 + y + 1)}
}
