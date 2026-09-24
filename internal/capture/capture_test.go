package capture

import (
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/proto"
)

// TestItemsAreCheckedAndReadAsAnAgentReadsThem: each kind needs its own
// field; an element reads with its page, selector, tag, attributes and text
// in the plain text an agent is handed. If it regresses, an empty capture
// goes to an agent, or an element arrives without saying where it was.
func TestItemsAreCheckedAndReadAsAnAgentReadsThem(t *testing.T) {
	for _, bad := range []proto.ContextItem{
		{Kind: KindURL}, {Kind: KindText, Text: "  "}, {Kind: KindFile}, {Kind: KindElement}, {Kind: "screenshot", Text: "x"},
		{Kind: KindText, Text: strings.Repeat("a", MaxText+1)},
	} {
		if Check(bad) == nil {
			t.Errorf("accepted %+v", bad.Kind)
		}
	}
	el := proto.ContextItem{Kind: KindElement, Source: "browser", Title: "Login", URL: "https://example.com/login",
		Selector: "#email", Tag: "input", Attributes: map[string]string{"type": "email", "id": "email"}}
	if err := Check(el); err != nil {
		t.Fatal(err)
	}
	got := Format([]proto.ContextItem{el, {Kind: KindFile, Path: "/p/main.go"}})
	for _, want := range []string{"Context captured in tend:", "[1] element (from browser)", "url: https://example.com/login",
		"selector: #email", "tag: input", `attributes: id="email" type="email"`, "[2] file", "path: /p/main.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if s := Summary(proto.ContextItem{Kind: KindElement, Selector: "button#save", Text: "Salvar"}); s != "button#save “Salvar”" {
		t.Errorf("summary: %q", s)
	}
}
