package config

import (
	"errors"
	"fmt"
	"strings"
)

// The title tend writes to the terminal it runs in, which is what a window
// manager shows in its title, tab and group bars. A pane's own OSC 0 stops at
// tend — tend is the terminal it writes to — so without this the outer window
// keeps whatever the shell or ssh last left there.
//
// It is herdr's `ui.window_title` (`config/window_title.rs`): the same tokens,
// the same escapes, the same default, and "" to leave the title alone.

// DefaultWindowTitle is herdr's.
const DefaultWindowTitle = "{hostname}: {workspace}"

// MaxWindowTitle is the most characters a title may run to, as in herdr: a
// program's title is not bounded, and a window bar is.
const MaxWindowTitle = 200

// WindowTitleToken is something a template can name.
type WindowTitleToken string

const (
	// TitleHostname is the machine the server runs on, which is the one the
	// panes live on even when this client is somewhere else.
	TitleHostname WindowTitleToken = "hostname"
	// TitleWorkspace is the active space's name.
	TitleWorkspace WindowTitleToken = "workspace"
	// TitleTab is the active tab's name.
	TitleTab WindowTitleToken = "tab"
	// TitlePane is the name the user gave the focused pane.
	TitlePane WindowTitleToken = "pane"
	// TitleTerminal is the focused pane's own title, spinner stripped.
	TitleTerminal WindowTitleToken = "terminal_title"
)

// WindowTitlePart is a run of text or a token.
type WindowTitlePart struct {
	Literal string
	Token   WindowTitleToken
}

// WindowTitleTemplate is a parsed `ui.window_title`.
type WindowTitleTemplate struct {
	Parts []WindowTitlePart
}

// ParseWindowTitle reads a template. An empty one parses to nil, meaning tend
// leaves the outer title alone.
func ParseWindowTitle(template string) (*WindowTitleTemplate, error) {
	if template == "" {
		return nil, nil
	}
	var parts []WindowTitlePart
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			parts = append(parts, WindowTitlePart{Literal: literal.String()})
			literal.Reset()
		}
	}
	runes := []rune(template)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; {
		case r == '{' && i+1 < len(runes) && runes[i+1] == '{':
			literal.WriteRune('{')
			i++
		case r == '}' && i+1 < len(runes) && runes[i+1] == '}':
			literal.WriteRune('}')
			i++
		case r == '{':
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					end = j
					break
				}
			}
			if end < 0 {
				return nil, errors.New("has an unclosed '{'")
			}
			name := string(runes[i+1 : end])
			token := WindowTitleToken(strings.TrimSpace(name))
			switch token {
			case TitleHostname, TitleWorkspace, TitleTab, TitlePane, TitleTerminal:
			default:
				return nil, fmt.Errorf("has unknown token '{%s}'", name)
			}
			flush()
			parts = append(parts, WindowTitlePart{Token: token})
			i = end
		case r == '}':
			return nil, errors.New("has an unmatched '}'")
		default:
			literal.WriteRune(r)
		}
	}
	flush()
	if len(parts) == 0 {
		return nil, nil
	}
	return &WindowTitleTemplate{Parts: parts}, nil
}

// Render fills the template in. A token with no value contributes nothing,
// and a title that comes out empty is reported as none.
func (t *WindowTitleTemplate) Render(value func(WindowTitleToken) string) (string, bool) {
	if t == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range t.Parts {
		if part.Token != "" {
			b.WriteString(value(part.Token))
			continue
		}
		b.WriteString(part.Literal)
	}
	return SanitizeWindowTitle(b.String())
}

// SanitizeWindowTitle makes text safe to put in an OSC 0: nothing that would
// end the sequence early, no control characters, and no more than a window
// bar can use. The text comes from pane titles, which programs choose.
func SanitizeWindowTitle(text string) (string, bool) {
	var b strings.Builder
	n := 0
	for _, r := range text {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			continue
		}
		if n == MaxWindowTitle {
			break
		}
		b.WriteRune(r)
		n++
	}
	out := strings.TrimSpace(b.String())
	return out, out != ""
}
