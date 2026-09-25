package capture

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
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

// TestTextTypedIntoAPaneCannotAct: a page's text loses every control
// character — an escape that would end a paste, a bell, a Ctrl+C — and keeps
// its line breaks only for a program that takes pastes; for one that does
// not, the lines are joined, since each break would be Enter. If it
// regresses, sending a page's element into a shell runs its text as
// commands, or text crafted to leave a paste acts as keys in an agent.
func TestTextTypedIntoAPaneCannotAct(t *testing.T) {
	evil := "line one\r\nrm -rf ~\x1b[201~\nnext\x03\x07\tend"
	pasted := Inert(evil, true)
	if strings.ContainsAny(pasted, "\x1b\x03\x07\r\t") || !strings.Contains(pasted, "line one\nrm -rf ~[201~\nnext end") {
		t.Errorf("for a paste: %q", pasted)
	}
	typed := Inert(evil, false)
	if strings.ContainsAny(typed, "\n\r\x1b\x03") || typed != "line one ⏎ rm -rf ~[201~ ⏎ next end" {
		t.Errorf("for a shell: %q", typed)
	}
}
