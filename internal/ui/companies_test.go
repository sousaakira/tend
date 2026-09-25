package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheCompaniesPanelIsClickedWhereItIsDrawn: the list shows every space
// first, the chosen company marked and a company's waiting agents said;
// each button and each line is found by a click on the place it is drawn,
// in the list and in a company's spaces. If it regresses, a click on
// "[ New ]" deletes a company, or a click on a line picks the one beneath.
func TestTheCompaniesPanelIsClickedWhereItIsDrawn(t *testing.T) {
	v := &CompaniesView{Active: 7, Cursor: 1, Entries: []CompanyEntry{
		{Spaces: 4, Waiting: 1}, {ID: 7, Name: "Acme", Spaces: 2}, {ID: 9, Name: "Globex", Spaces: 2, Waiting: 1},
	}}
	g := vt.NewGrid(120, 36, 0)
	Draw(g, Frame{Companies: v}, DefaultTheme())
	lines := gridText(g)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"COMPANIES", "all spaces", "● Acme", "1 waiting · 2 spaces", "[ Switch ]", "[ Delete ]", "[ Close ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	at := func(r Rect) string { return string([]rune(lines[r.Y])[r.X : r.X+r.Cols]) }
	geo := CompaniesLayout(v, 120, 36)
	for i, label := range CompanyButtons {
		if got := at(geo.Buttons[i]); got != label {
			t.Errorf("button %d is %q where %q is drawn", i, label, got)
		}
	}
	for i, name := range []string{"all spaces", "Acme", "Globex"} {
		y := geo.List.Y + i
		if got, ok := CompaniesLineAt(v, 120, 36, geo.List.X+3, y); !ok || got != i || !strings.Contains(lines[y], name) {
			t.Errorf("line %d (%q) is found as %d %v: %q", i, name, got, ok, lines[y])
		}
	}

	v.Choosing, v.ChoosingName = 7, "Acme"
	v.Spaces = []CompanySpace{{ID: 1, Name: "api", In: true}, {ID: 2, Name: "site"}}
	g.Clear(vt.DefaultStyle)
	Draw(g, Frame{Companies: v}, DefaultTheme())
	lines = gridText(g)
	geo = CompaniesLayout(v, 120, 36)
	if !strings.Contains(strings.Join(lines, "\n"), "SPACES OF Acme") || len(geo.Buttons) != 1 || at(geo.Buttons[0]) != "[ Done ]" {
		t.Errorf("a company's spaces:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[geo.List.Y], "[✓] api") || !strings.Contains(lines[geo.List.Y+1], "[ ] site") {
		t.Errorf("ticks: %q %q", lines[geo.List.Y], lines[geo.List.Y+1])
	}
	if got, ok := CompaniesLineAt(v, 120, 36, geo.List.X, geo.List.Y+1); !ok || got != 1 {
		t.Errorf("the second space is found as %d %v", got, ok)
	}
}

// TestTheKeyHelpFitsAShortTerminal: on a terminal twenty rows high every
// line of the key help is on screen, in as many columns as it takes, cut
// short when the columns would not fit side by side. If it regresses, the
// help's last keys — detach, this help — run off the bottom of the box,
// as they did when the companies key was added.
func TestTheKeyHelpFitsAShortTerminal(t *testing.T) {
	lines := append([]string{"tend"}, HelpLines()...)
	g := vt.NewGrid(110, 20, 0)
	Draw(g, Frame{Overlay: lines}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, line := range lines[1:] {
		head := strings.TrimRight(string([]rune(line)[:min(len([]rune(line)), 14)]), " ")
		if !strings.Contains(text, head) {
			t.Errorf("%q is not on screen:\n%s", head, text)
		}
	}
}
