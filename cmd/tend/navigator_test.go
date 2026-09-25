//go:build unix

package main

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheNavigatorFindsAPaneByNameAndGoesThere: prefix+g puts herdr's
// navigator up over the screen, / and a few letters find a pane by its
// name, and enter goes to it. If it regresses, the pane wanted has to be
// hunted for tab by tab.
func TestTheNavigatorFindsAPaneByNameAndGoesThere(t *testing.T) {
	a := startSession(t, 110, 28)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf FIRST-SPACE\n", "the marker", func(s string) bool { return strings.Contains(s, "FIRST-SPACE") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "the second space", func(s string) bool { return strings.Contains(s, "space 2") && !strings.Contains(s, "FIRST-SPACE") })

	a.send(t, "\x02g")
	a.waitForScreen(t, "the navigator", func(s string) bool {
		return strings.Contains(s, "/ search panes") && strings.Contains(s, "◆") && strings.Contains(s, "filter a/b/w/i/d")
	})

	// Nothing is blocked: the filter leaves nothing, and a shows all again.
	a.send(t, "b")
	a.waitForScreen(t, "the blocked filter", func(s string) bool { return strings.Contains(s, "/ blocked") && strings.Contains(s, "nothing matches") })
	a.send(t, "a")
	a.waitForScreen(t, "everything again", func(s string) bool { return strings.Contains(s, "/ search panes") })

	// The first space is "main"; its pane is found by the space's name.
	a.send(t, "/main")
	a.waitForScreen(t, "the search", func(s string) bool { return strings.Contains(s, "/ main") })
	a.send(t, "\x1b[B\x1b[B\r")
	a.waitForScreen(t, "the first space's pane", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE") && !strings.Contains(s, "search panes") && !strings.Contains(s, "/ main")
	})
}

// TestTheNavigatorAnswersTheMouse: a click on a row goes there, and a click
// outside puts the navigator away. If it regresses, the popup can only be
// driven from the keyboard, or cannot be dismissed with the mouse.
func TestTheNavigatorAnswersTheMouse(t *testing.T) {
	a := startSession(t, 110, 28)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf FIRST-SPACE\n", "the marker", func(s string) bool { return strings.Contains(s, "FIRST-SPACE") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "the second space", func(s string) bool { return strings.Contains(s, "space 2") && !strings.Contains(s, "FIRST-SPACE") })

	a.send(t, "\x02g")
	a.waitForScreen(t, "the navigator", func(s string) bool { return strings.Contains(s, "/ search panes") })
	a.clickAt(t, 1, 1)
	a.waitForScreen(t, "the navigator to go", func(s string) bool { return !strings.Contains(s, "/ search panes") })

	a.send(t, "\x02g")
	a.waitForScreen(t, "the navigator again", func(s string) bool { return strings.Contains(s, "/ search panes") })
	// The first space's row: "▾ main", the first row of the list.
	row := a.lineContaining(t, "▾ main")
	col := strings.Index(a.lines()[row-1], "main")
	a.clickAt(t, len([]rune(a.lines()[row-1][:col]))+2, row)
	a.waitForScreen(t, "the first space", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE") && !strings.Contains(s, "/ search panes")
	})
}

// TestPointerMovesReachAProgramThatAskedForThem: a program in view that asked
// for every pointer move (DECSET 1003, as Claude Code does for hover) gets
// them, which means the client turns motion on at the outer terminal and
// forwards a move to the pane under it; with no such program, motion stays
// off. If it regresses, hover in Claude Code never happens under tend.
func TestPointerMovesReachAProgramThatAskedForThem(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.mu.Lock()
	early := a.screen.Modes().Mouse
	a.mu.Unlock()
	if early == vt.MouseAnyEvent {
		t.Fatal("motion is on with no program asking for it")
	}
	// A program that asks for motion in SGR and shows what it receives.
	a.sendUntil(t, `printf 'READY\n\033[?1003h\033[?1006h'; stty raw -echo; cat -v`+"\n", "the program",
		func(s string) bool { return strings.Contains(s, "READY") })
	a.waitForScreen(t, "motion on at the outer terminal", func(string) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.screen.Modes().Mouse == vt.MouseAnyEvent
	})
	// A move with no button over the pane: SGR button 35.
	a.sendUntil(t, "\x1b[<35;40;10M", "the move to reach the program", func(s string) bool { return strings.Contains(s, "^[[<35;") })
}
