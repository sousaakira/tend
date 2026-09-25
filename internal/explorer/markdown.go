package explorer

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// Markdown in the preview is shown as it reads rather than as it is written,
// herdr-sidebar's rendered preview (which it gets from glow): headings in
// colour without their hashes, emphasis as emphasis, code set apart, links
// as their text, lists with bullets, quotes with a bar. It is the common
// part of CommonMark, line by line; m shows the source instead.

var (
	mdH1     = vt.Style{FG: vt.IndexedColor(5), Attrs: vt.AttrBold | vt.AttrUnderline}
	mdH2     = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold}
	mdH3     = vt.Style{FG: vt.IndexedColor(6), Attrs: vt.AttrBold}
	mdCode   = vt.Style{FG: vt.IndexedColor(3)}
	mdLink   = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrUnderline}
	mdQuote  = vt.Style{FG: vt.IndexedColor(8), Attrs: vt.AttrItalic}
	mdBullet = vt.Style{FG: vt.IndexedColor(5), Attrs: vt.AttrBold}
	mdRule   = styleDim
)

// renderMarkdown turns markdown source into lines of spans, width wide for
// the rules.
func renderMarkdown(src []string, width int) [][]span {
	var out [][]span
	fence := false
	for _, line := range src {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence = !fence
			// The fence itself is not shown; a code block reads as indented.
			if fence && len(trimmed) > 3 {
				out = append(out, []span{{"  " + strings.Trim(trimmed, "`~ "), styleDim}})
			}
			continue
		case fence:
			out = append(out, []span{{"  " + line, mdCode}})
			continue
		case trimmed == "":
			out = append(out, nil)
			continue
		}

		switch {
		case isRule(trimmed):
			out = append(out, []span{{strings.Repeat("─", max(width-2, 3)), mdRule}})
		case strings.HasPrefix(trimmed, "#"):
			level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			text := strings.TrimSpace(strings.TrimRight(trimmed[level:], "#"))
			style := mdH3
			switch level {
			case 1:
				style = mdH1
				text = strings.ToUpper(text)
			case 2:
				style = mdH2
			}
			out = append(out, inline(text, style))
		case strings.HasPrefix(trimmed, ">"):
			text := strings.TrimSpace(strings.TrimLeft(trimmed, ">"))
			out = append(out, append([]span{{"▎ ", mdQuote}}, inline(text, mdQuote)...))
		case isBullet(trimmed):
			indent := strings.Repeat("  ", (len(line)-len(strings.TrimLeft(line, " ")))/2)
			text := strings.TrimSpace(trimmed[2:])
			switch {
			case strings.HasPrefix(text, "[ ] "):
				out = append(out, append([]span{{indent + "☐ ", mdBullet}}, inline(text[4:], styleNormal)...))
			case strings.HasPrefix(text, "[x] ") || strings.HasPrefix(text, "[X] "):
				out = append(out, append([]span{{indent + "☑ ", mdBullet}}, inline(text[4:], styleDim)...))
			default:
				out = append(out, append([]span{{indent + "• ", mdBullet}}, inline(text, styleNormal)...))
			}
		default:
			if n, rest, ok := orderedItem(trimmed); ok {
				indent := strings.Repeat("  ", (len(line)-len(strings.TrimLeft(line, " ")))/2)
				out = append(out, append([]span{{indent + n + " ", mdBullet}}, inline(rest, styleNormal)...))
				continue
			}
			out = append(out, inline(trimmed, styleNormal))
		}
	}
	return out
}

func isRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for _, r := range s {
		if r != rune(c) && r != ' ' {
			return false
		}
	}
	return true
}

func isBullet(s string) bool {
	return len(s) > 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' '
}

// orderedItem reads "12. text".
func orderedItem(s string) (number, rest string, ok bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
		return "", "", false
	}
	return s[:i+1], strings.TrimSpace(s[i+2:]), true
}

// inline renders emphasis, code and links in a line of text set in base.
func inline(text string, base vt.Style) []span {
	var out []span
	emit := func(s string, style vt.Style) {
		if s != "" {
			out = append(out, span{s, style})
		}
	}
	var plain strings.Builder
	flush := func() {
		emit(plain.String(), base)
		plain.Reset()
	}
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case rest[0] == '`':
			if end := strings.IndexByte(rest[1:], '`'); end >= 0 {
				flush()
				emit(rest[1:1+end], mdCode)
				i += end + 2
				continue
			}
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			if end := strings.Index(rest[2:], rest[:2]); end > 0 {
				flush()
				style := base
				style.Attrs |= vt.AttrBold
				out = append(out, inlineStyled(rest[2:2+end], style)...)
				i += end + 4
				continue
			}
		case (rest[0] == '*' || rest[0] == '_') && len(rest) > 1 && !unicode.IsSpace(rune(rest[1])):
			// An underscore inside a word is part of it (snake_case).
			if rest[0] == '_' && i > 0 && isWordByte(text[i-1]) {
				break
			}
			if end := strings.IndexByte(rest[1:], rest[0]); end > 0 {
				flush()
				style := base
				style.Attrs |= vt.AttrItalic
				emit(rest[1:1+end], style)
				i += end + 2
				continue
			}
		case rest[0] == '[' || strings.HasPrefix(rest, "!["):
			img := rest[0] == '!'
			open := 0
			if img {
				open = 1
			}
			closeText := strings.Index(rest, "](")
			if closeText > open {
				if end := strings.IndexByte(rest[closeText:], ')'); end > 0 {
					flush()
					label := rest[open+1 : closeText]
					if img {
						label = "▣ " + label
					}
					emit(label, mdLink)
					i += closeText + end + 1
					continue
				}
			}
		}
		plain.WriteByte(text[i])
		i++
	}
	flush()
	return out
}

// inlineStyled renders what is inside bold, where code and links still
// read as themselves.
func inlineStyled(text string, style vt.Style) []span {
	spans := inline(text, style)
	for i := range spans {
		if spans[i].style == style {
			continue
		}
		spans[i].style.Attrs |= style.Attrs
	}
	return spans
}

// spansWidth is how many columns a line of spans takes.
func spansWidth(line []span) int {
	w := 0
	for _, s := range line {
		w += runewidth.StringWidth(s.text)
	}
	return w
}
