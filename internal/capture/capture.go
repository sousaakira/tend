// Package capture is the context buffer's vocabulary: what a captured item
// may be, and how items read when they are handed to an agent or copied.
//
// The buffer itself is the server's (internal/server/context.go): tools put
// things in it over the socket, and it is from there that they reach an
// agent, so no tool talks to any agent directly and any tool's capture can
// go to any agent. This package is only what both ends agree on.
package capture

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sousaakira/tend/internal/proto"
)

// The kinds of item. A browser extension sends elements and URLs; the files
// panel files; anything can send text.
const (
	KindURL     = "url"
	KindElement = "element"
	KindText    = "text"
	KindFile    = "file"
)

// Kinds are every kind, for a message that lists them.
var Kinds = []string{KindURL, KindElement, KindText, KindFile}

// ErrEmpty is an item that carries nothing to hand over.
var ErrEmpty = errors.New("capture: the item has nothing in it")

// MaxText bounds an item's text: a whole page pasted by mistake should not
// become a prompt of megabytes.
const MaxText = 64 << 10

// Check is an item fit for the buffer: a known kind, with the field the
// kind is about, its text within MaxText.
func Check(it proto.ContextItem) error {
	switch it.Kind {
	case KindURL:
		if it.URL == "" {
			return fmt.Errorf("%w: a url item needs its url", ErrEmpty)
		}
	case KindElement:
		if it.Selector == "" && it.Text == "" && it.Tag == "" {
			return fmt.Errorf("%w: an element needs its selector, tag or text", ErrEmpty)
		}
	case KindText:
		if strings.TrimSpace(it.Text) == "" {
			return fmt.Errorf("%w: a text item needs its text", ErrEmpty)
		}
	case KindFile:
		if it.Path == "" {
			return fmt.Errorf("%w: a file item needs its path", ErrEmpty)
		}
	default:
		return fmt.Errorf("capture: kind %q; use %s", it.Kind, strings.Join(Kinds, ", "))
	}
	if len(it.Text) > MaxText {
		return fmt.Errorf("capture: %d bytes of text, more than %d", len(it.Text), MaxText)
	}
	return nil
}

// Summary is an item in one line, for a list.
func Summary(it proto.ContextItem) string {
	switch it.Kind {
	case KindURL:
		if it.Title != "" {
			return it.Title + " — " + it.URL
		}
		return it.URL
	case KindElement:
		what := it.Selector
		if what == "" {
			what = it.Tag
		}
		if text := oneLine(it.Text); text != "" {
			what += " “" + text + "”"
		}
		return what
	case KindFile:
		return it.Path
	}
	return oneLine(it.Text)
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

// Format is items as an agent is handed them: a line saying what follows,
// then each item with what it has, in plain text a prompt can hold.
func Format(items []proto.ContextItem) string {
	var b strings.Builder
	b.WriteString("Context captured in tend:\n")
	for i, it := range items {
		fmt.Fprintf(&b, "\n[%d] %s", i+1, it.Kind)
		if it.Source != "" {
			fmt.Fprintf(&b, " (from %s)", it.Source)
		}
		b.WriteString("\n")
		field := func(name, value string) {
			if value != "" {
				fmt.Fprintf(&b, "%s: %s\n", name, value)
			}
		}
		field("title", it.Title)
		field("url", it.URL)
		field("selector", it.Selector)
		field("tag", it.Tag)
		field("path", it.Path)
		if len(it.Attributes) > 0 {
			names := make([]string, 0, len(it.Attributes))
			for name := range it.Attributes {
				names = append(names, name)
			}
			sort.Strings(names)
			pairs := make([]string, 0, len(names))
			for _, name := range names {
				pairs = append(pairs, name+"="+fmt.Sprintf("%q", it.Attributes[name]))
			}
			field("attributes", strings.Join(pairs, " "))
		}
		if it.Text != "" {
			if strings.Contains(it.Text, "\n") {
				b.WriteString("text:\n" + it.Text + "\n")
			} else {
				field("text", it.Text)
			}
		}
	}
	return b.String()
}
