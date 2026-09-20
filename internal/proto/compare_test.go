package proto

import "testing"

// TestCompareFindsWhichSideIsBehind: builds are git descriptions with no
// ordering, so what one side can do and the other has never heard of is the
// only fact with a direction in it.
func TestCompareFindsWhichSideIsBehind(t *testing.T) {
	// A server of this build knows exactly what this build knows.
	if ahead, behind := Compare(KnownMethods); ahead || behind {
		t.Errorf("same build: serverAhead=%v clientAhead=%v", ahead, behind)
	}

	// A server missing something this client uses is the older half.
	var without []string
	for _, m := range KnownMethods {
		if m != MethodPaneText {
			without = append(without, m)
		}
	}
	if ahead, behind := Compare(without); ahead || !behind {
		t.Errorf("server missing a method: serverAhead=%v clientAhead=%v", ahead, behind)
	}

	// A server offering something this client has never heard of is newer.
	if ahead, behind := Compare(append(append([]string{}, KnownMethods...), "pane.teleport")); !ahead || behind {
		t.Errorf("server with an extra: serverAhead=%v clientAhead=%v", ahead, behind)
	}

	// Each having something the other lacks is divergence, not age.
	ahead, behind := Compare(append(without, "pane.teleport"))
	if !ahead || !behind {
		t.Errorf("diverged: serverAhead=%v clientAhead=%v", ahead, behind)
	}
}

// TestKnownMethodsIsComplete keeps the list from drifting: a method added to
// the protocol and not to it makes every older server look current.
func TestKnownMethodsIsComplete(t *testing.T) {
	seen := make(map[string]bool, len(KnownMethods))
	for _, m := range KnownMethods {
		if seen[m] {
			t.Errorf("%q is listed twice", m)
		}
		seen[m] = true
	}
}
