package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheContextPanelShowsAnItemsParts: the list of captures, the chosen
// one's parts under their headings, where Send to Agent types, and the
// buttons a click finds; an empty buffer says how to fill it. If it
// regresses, an item is sent without anyone seeing what it holds.
func TestTheContextPanelShowsAnItemsParts(t *testing.T) {
	v := &ContextView{Target: "1 claude", Items: []ContextEntry{
		{Kind: "element", Summary: "button#save “Salvar”", Parts: []ContextPart{
			{Heading: "URL", Value: "https://example.com/dashboard"},
			{Heading: "SELECTED ELEMENT", Value: "button#save"},
			{Heading: "TEXT", Value: "Salvar"},
		}},
		{Kind: "file", Summary: "/p/main.go"},
	}}
	g := vt.NewGrid(100, 32, 0)
	Draw(g, Frame{Context: v}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"CONTEXT", "[element] button#save", "[file] /p/main.go", "URL", "https://example.com/dashboard",
		"SELECTED ELEMENT", "TEXT", "Salvar", "types it into 1 claude", "[ Copy ]", "[ Send ]", "[ Send all ]", "[ Close ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for b := ContextCopy; b <= ContextClose; b++ {
		r := ContextLayout(v, 100, 32).Buttons[b]
		if got, ok := ContextButtonAt(v, 100, 32, r.X+1, r.Y); !ok || got != b {
			t.Errorf("button %d found as %d %v", b, got, ok)
		}
	}
	geo := ContextLayout(v, 100, 32)
	if i, ok := ContextItemAt(v, 100, 32, geo.List.X+2, geo.List.Y+1); !ok || i != 1 {
		t.Errorf("the second line is the second item: %d %v", i, ok)
	}

	g.Clear(vt.DefaultStyle)
	Draw(g, Frame{Context: &ContextView{}}, DefaultTheme())
	if text := strings.Join(gridText(g), "\n"); !strings.Contains(text, "Nothing captured yet") || !strings.Contains(text, "tend context add") {
		t.Errorf("empty:\n%s", text)
	}
}
