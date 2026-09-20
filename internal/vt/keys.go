package vt

import "strings"

// A program driving a pane from a script names the key it wants — "enter",
// "ctrl+c", "up" — and something has to turn that into the bytes a terminal
// would have sent. A person typing needs none of this: their terminal encodes
// the key and the client forwards the bytes. This is the same encoding, done
// for a caller who has only a name.
//
// It belongs here because the answer depends on the terminal's own state: an
// application asking for DECCKM gets ESC O A for the up arrow instead of
// ESC [ A, and a program that reads the wrong one sees a stray letter in its
// input. The names are herdr's (`config::parse_key_combo` plus its API
// aliases), so a script written for one works against the other.

// namedKeys are the keys whose bytes do not depend on any mode.
var namedKeys = map[string]string{
	"enter":     "\r",
	"return":    "\r",
	"tab":       "\t",
	"backspace": "\x7f",
	"escape":    "\x1b",
	"esc":       "\x1b",
	"space":     " ",
	"plus":      "+",
	"delete":    "\x1b[3~",
	"insert":    "\x1b[2~",
	"pageup":    "\x1b[5~",
	"pagedown":  "\x1b[6~",
	"f1":        "\x1bOP",
	"f2":        "\x1bOQ",
	"f3":        "\x1bOR",
	"f4":        "\x1bOS",
	"f5":        "\x1b[15~",
	"f6":        "\x1b[17~",
	"f7":        "\x1b[18~",
	"f8":        "\x1b[19~",
	"f9":        "\x1b[20~",
	"f10":       "\x1b[21~",
	"f11":       "\x1b[23~",
	"f12":       "\x1b[24~",
}

// cursorKeys are the keys whose final letter is the same in both encodings,
// and only the prefix changes with DECCKM.
var cursorKeys = map[string]byte{
	"up":    'A',
	"down":  'B',
	"right": 'C',
	"left":  'D',
	"home":  'H',
	"end":   'F',
}

// EncodeKey turns a key's name into the bytes a terminal would send for it,
// under the modes the program has asked for. ok is false for a name it does
// not know, which is a caller's mistake worth reporting rather than sending
// something arbitrary.
func EncodeKey(name string, m Modes) (bytes []byte, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, false
	}
	// herdr's two aliases, for the shorthand people actually type.
	switch name {
	case "c-c":
		name = "ctrl+c"
	case "+":
		name = "plus"
	}

	if rest, cut := strings.CutPrefix(name, "ctrl+"); cut {
		return encodeCtrl(rest)
	}
	if rest, cut := strings.CutPrefix(name, "alt+"); cut {
		// Alt is the same key with an escape in front of it, which is what
		// every terminal in this lineage sends.
		inner, ok := EncodeKey(rest, m)
		if !ok {
			return nil, false
		}
		return append([]byte{0x1b}, inner...), true
	}
	if rest, cut := strings.CutPrefix(name, "shift+"); cut {
		if rest == "tab" {
			return []byte("\x1b[Z"), true // back-tab, its own sequence
		}
		if final, is := cursorKeys[rest]; is {
			return []byte{0x1b, '[', '1', ';', '2', final}, true
		}
		if len(rest) == 1 {
			return []byte(strings.ToUpper(rest)), true
		}
		return nil, false
	}

	if b, is := namedKeys[name]; is {
		return []byte(b), true
	}
	if final, is := cursorKeys[name]; is {
		if m.ApplicationCur {
			return []byte{0x1b, 'O', final}, true
		}
		return []byte{0x1b, '[', final}, true
	}
	// A single character is itself. Anything longer is a name nobody defined.
	if r := []rune(name); len(r) == 1 {
		return []byte(name), true
	}
	return nil, false
}

// encodeCtrl produces the control character for ctrl+<key>.
func encodeCtrl(key string) ([]byte, bool) {
	r := []rune(key)
	if len(r) != 1 {
		return nil, false
	}
	c := r[0]
	switch {
	case c >= 'a' && c <= 'z':
		return []byte{byte(c-'a') + 1}, true
	case c >= 'A' && c <= 'Z':
		return []byte{byte(c-'A') + 1}, true
	case c == '[':
		return []byte{0x1b}, true
	case c == '\\':
		return []byte{0x1c}, true
	case c == ']':
		return []byte{0x1d}, true
	case c == '^':
		return []byte{0x1e}, true
	case c == '_':
		return []byte{0x1f}, true
	case c == '@' || c == ' ':
		return []byte{0}, true
	case c == '?':
		return []byte{0x7f}, true
	}
	return nil, false
}

// EncodeText turns text into the bytes a paste of it would produce.
//
// Wrapped in bracketed paste markers when the program asked for them. It is
// how a program tells typing from pasting: an editor inserts a pasted newline
// instead of running the line, and an agent's prompt box keeps a multi-line
// answer in one message rather than sending the first line on its own.
func EncodeText(text string, m Modes) []byte {
	if !m.BracketedPaste {
		return []byte(text)
	}
	out := make([]byte, 0, len(text)+12)
	out = append(out, "\x1b[200~"...)
	out = append(out, text...)
	return append(out, "\x1b[201~"...)
}
