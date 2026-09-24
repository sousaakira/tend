package ui

import (
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/vt"
)

func toolbarFrame(width int) Frame {
	return Frame{
		Sidebar: true, SidebarWidth: width,
		Toolbar: []ToolbarItem{{ID: ToolFiles, Active: true}, {ID: ToolAgents, Disabled: true},
			{ID: ToolBrowser, Disabled: true}, {ID: ToolContext, Disabled: true}},
		ToolbarIcons: "emoji",
		Spaces:       SidebarSection{Rows: []SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}},
	}
}

// TestTheToolbarSitsOverTheSpaces: the tools are the sidebar's first line,
// a rule the second, and the spaces start under them; a click finds each
// tool where it is drawn, and at the narrowest sidebar all four still fit.
// If it regresses, the toolbar is drawn over the spaces' heading, or a
// click on an icon lands on the one beside it.
func TestTheToolbarSitsOverTheSpaces(t *testing.T) {
	const rows = 24
	f := toolbarFrame(SidebarWidth)
	g := vt.NewGrid(80, rows, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[0], "📁") || !strings.Contains(lines[0], "📋") {
		t.Errorf("the tools on the first line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "────") {
		t.Errorf("a rule under them: %q", lines[1])
	}
	if !strings.Contains(lines[2], "spaces") {
		t.Errorf("the spaces under the rule: %q", lines[2])
	}
	spaces, _ := SidebarRegions(f, rows)
	if spaces.Y != ToolbarRows {
		t.Errorf("the spaces' region starts at %d", spaces.Y)
	}
	for i, z := range ToolbarLayout(f) {
		if got, ok := ToolbarItemAt(f, z.From, 0, rows); !ok || got != i {
			t.Errorf("a click on tool %d found %d %v", i, got, ok)
		}
		if SidebarPlaceAt(f, z.From, 0, rows) != SidebarToolbar {
			t.Errorf("tool %d is the toolbar's place", i)
		}
	}
	if _, ok := ToolbarItemAt(f, 2, 1, rows); ok {
		t.Error("the rule is no tool")
	}

	narrow := toolbarFrame(SidebarMinWidth)
	if zones := ToolbarLayout(narrow); len(zones) != 4 || zones[3].To > SidebarMinWidth-1 {
		t.Errorf("all four in the narrowest sidebar, inside its edge: %+v", zones)
	}

	f.Toolbar = nil
	if spaces, _ := SidebarRegions(f, rows); spaces.Y != 0 {
		t.Errorf("without a toolbar the spaces are at the top: %d", spaces.Y)
	}
}
