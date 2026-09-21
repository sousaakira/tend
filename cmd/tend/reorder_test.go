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

// TestNavigationKeysReachPanesAndAgents covers the keys that move focus
// without the mouse: prefix+tab through the tab's panes, prefix+; back to the
// last one, and prefix+< / > through the agents wherever they are. If they
// regress, reaching the agent that stopped means the sidebar or nothing.
func TestNavigationKeysReachPanesAndAgents(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Two panes, the second focused.
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool { return strings.Count(s, "┌") == 2 })
	a.sendUntil(t, "printf 'IN-SECOND\\n'\n", "the second pane", func(s string) bool {
		return strings.Contains(s, "IN-SECOND")
	})

	// prefix+tab cycles to the first, and typing lands there.
	a.send(t, "\x02\t")
	a.sendUntil(t, "printf 'IN-FIRST\\n'\n", "the first pane", func(s string) bool {
		return strings.Contains(s, "IN-FIRST")
	})

	// prefix+; goes back to where focus was before — the right-hand pane,
	// which is the side of the divider the mark has to land on.
	divider := a.dividerColumn()
	if divider < 0 {
		t.Fatalf("no divider to tell the panes apart:\n%s", a.text())
	}
	a.send(t, "\x02;")
	a.sendUntil(t, "printf 'BACK-AGAIN\\n'\n", "the pane focused before", func(s string) bool {
		for _, line := range strings.Split(s, "\n") {
			if i := strings.Index(line, "BACK-AGAIN"); i > divider {
				return true
			}
		}
		return false
	})

	// An agent, reached from anywhere with prefix+>.
	prog := fakeAgentBin(t, "claude", "printf 'THE-AGENT-IS-HERE\\n'; cat")
	a.sendUntil(t, prog+"\n", "the agent", func(s string) bool {
		return strings.Contains(s, "THE-AGENT-IS-HERE")
	})
	a.send(t, "\x02\t") // away from it
	a.waitForScreen(t, "focus to move", func(string) bool { return true })
	a.send(t, "\x02>")
	a.sendUntil(t, "to-the-agent\n", "the agent's pane to take the typing", func(s string) bool {
		return strings.Contains(s, "to-the-agent")
	})
}
