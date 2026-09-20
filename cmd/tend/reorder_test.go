package main

import (
	"strings"
	"testing"
)

// TestSwappingPanesMovesTheProgramNotTheScreen: prefix+shift+l trades the
// focused pane with the one on its right, and what each pane shows goes with
// it. If it regresses, rearranging a layout means closing panes and losing
// what ran in them.
func TestSwappingPanesMovesTheProgramNotTheScreen(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'LEFT-PANE\\n'\n", "the left pane's mark", func(s string) bool {
		return strings.Count(s, "LEFT-PANE") >= 1
	})
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool { return strings.Count(s, "┌") == 2 })
	a.sendUntil(t, "printf 'RIGHT-PANE\\n'\n", "the right pane's mark", func(s string) bool {
		return strings.Contains(s, "RIGHT-PANE")
	})

	leftOf := func(s, mark string) bool {
		for _, line := range strings.Split(s, "\n") {
			l, r := strings.Index(line, "LEFT-PANE"), strings.Index(line, "RIGHT-PANE")
			if l >= 0 && r >= 0 {
				if mark == "LEFT-PANE" {
					return l < r
				}
				return r < l
			}
		}
		return false
	}
	// Both marks on one row, the right pane's output echoed at the same
	// height, is what lets the test tell which is where.
	if !leftOf(a.text(), "LEFT-PANE") {
		a.sendUntil(t, "printf 'RIGHT-PANE\\n'\n", "both marks side by side", func(s string) bool {
			return leftOf(s, "LEFT-PANE")
		})
	}

	// Focus is on the right pane. Swapping left puts it on the left.
	a.send(t, "\x02H")
	a.waitForScreen(t, "the panes to trade places", func(s string) bool {
		return leftOf(s, "RIGHT-PANE")
	})
}
