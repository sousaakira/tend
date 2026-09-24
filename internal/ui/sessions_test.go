package ui

import (
	"testing"
	"time"
)

// TestASessionSearchNeedsEveryWord: the search keeps an entry only when
// every word typed is somewhere in its title, directory, agent or id, in
// any case. If it regresses, "shop checkout" lists every checkout of every
// project, or nothing typed in capitals finds anything.
func TestASessionSearchNeedsEveryWord(t *testing.T) {
	all := []SessionEntry{
		{ID: "a", Title: "Checkout button", Dir: "/work/shop"},
		{ID: "b", Title: "Checkout flow", Dir: "/work/blog"},
		{ID: "c", Title: "Footer", Dir: "/work/shop"},
	}
	ids := func(list []SessionEntry) (out string) {
		for _, e := range list {
			out += e.ID
		}
		return out
	}
	for query, want := range map[string]string{"": "abc", "checkout": "ab", "SHOP checkout": "a", "shop": "ac", "nothing": ""} {
		if got := ids(FilterSessions(all, query)); got != want {
			t.Errorf("%q = %q, want %q", query, got, want)
		}
	}
}

// TestASessionsAgeIsShort: ages fit their column, and past two months are
// a date. If it regresses, the list's columns run into each other.
func TestASessionsAgeIsShort(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "now", 5 * time.Minute: "5m", 3 * time.Hour: "3h",
		4 * 24 * time.Hour: "4d", 90 * 24 * time.Hour: "Jun 26",
	} {
		if got := SessionAge(now, now.Add(-d)); got != want {
			t.Errorf("%v ago = %q, want %q", d, got, want)
		}
	}
}
