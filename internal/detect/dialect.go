package detect

import "strings"

// The manifests are written in Rust's regex dialect, because that is where
// they were calibrated. Rust's regex crate and Go's regexp are both RE2 — same
// engine family, no backreferences, no lookaround — so the semantics carry
// over unchanged. Two pieces of *syntax* do not:
//
//   - Rust accepts ￿ and \u{FFFF} for a codepoint; Go only accepts
//     \x{FFFF}.
//   - Rust supports Unicode properties such as \p{Alphabetic}; Go supports
//     only categories and scripts.
//
// Translating at load time rather than editing the manifests keeps them
// byte-identical to their upstream, so a future sync is a copy rather than a
// re-edit. The alternative — rewriting nine patterns by hand — would have to
// be redone every time upstream changes them.
//
// Anything not listed here is passed through untouched and left for Go's own
// compiler to reject, which fails loudly at load rather than quietly at
// runtime.

// goPropertyAlias maps Unicode properties Go lacks onto its nearest category.
//
// Alphabetic is not exactly L: it also covers Nl and Other_Alphabetic, which
// includes some Indic combining marks. Every manifest use is "a letter follows
// the spinner", where the difference cannot arise. The approximation is listed
// here rather than buried so that it is a decision someone can revisit.
var goPropertyAlias = map[string]string{
	"Alphabetic": "L",
	"alpha":      "L",
}

// translatePattern rewrites a manifest pattern from Rust's regex dialect into
// Go's.
func translatePattern(p string) string {
	if !strings.Contains(p, `\`) {
		return p
	}
	var b strings.Builder
	b.Grow(len(p))

	for i := 0; i < len(p); {
		c := p[i]
		if c != '\\' || i+1 >= len(p) {
			b.WriteByte(c)
			i++
			continue
		}
		switch p[i+1] {
		case 'u', 'U':
			if lit, n, ok := scanCodepoint(p[i:]); ok {
				b.WriteString(lit)
				i += n
				continue
			}
		case 'p', 'P':
			if lit, n, ok := scanProperty(p[i:]); ok {
				b.WriteString(lit)
				i += n
				continue
			}
		}
		// Copy the escape and the character it escapes together, so that a
		// literal backslash can never be read as opening a new escape.
		b.WriteByte(c)
		b.WriteByte(p[i+1])
		i += 2
	}
	return b.String()
}

// scanCodepoint translates ￿, \u{FFFF}, \UFFFFFFFF and \U{FFFFFFFF} into
// Go's \x{...}. It reports how many bytes of input it consumed.
func scanCodepoint(s string) (string, int, bool) {
	if len(s) < 3 {
		return "", 0, false
	}
	braced := s[2] == '{'
	if braced {
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return "", 0, false
		}
		digits := s[3:end]
		if !isHex(digits) {
			return "", 0, false
		}
		return `\x{` + digits + `}`, end + 1, true
	}

	width := 4 // ￿
	if s[1] == 'U' {
		width = 8 // \UFFFFFFFF
	}
	if len(s) < 2+width {
		return "", 0, false
	}
	digits := s[2 : 2+width]
	if !isHex(digits) {
		return "", 0, false
	}
	return `\x{` + digits + `}`, 2 + width, true
}

// scanProperty translates \p{Name} when Name is one Go does not know. The
// single-letter forms \pL and \p{L} already work and are left alone.
func scanProperty(s string) (string, int, bool) {
	if len(s) < 4 || s[2] != '{' {
		return "", 0, false
	}
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return "", 0, false
	}
	name := s[3:end]
	mapped, ok := goPropertyAlias[name]
	if !ok {
		return "", 0, false
	}
	return string(s[0]) + string(s[1]) + "{" + mapped + "}", end + 1, true
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9',
			c >= 'a' && c <= 'f',
			c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
