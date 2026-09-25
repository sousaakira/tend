//go:build unix

package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestACompanyChosenShowsItsSpacesOnly: prefix+O puts the companies panel
// up; "n" and a name make a company and offer its spaces to tick; choosing
// it empties the list of the spaces it does not have; a space made while
// it is chosen goes into it; the choice is still there when tend is opened
// again; and choosing all spaces brings the others back. If it regresses,
// a company made cannot be chosen, choosing one hides nothing or hides the
// space just made, or tend forgets the company it was left in.
func TestACompanyChosenShowsItsSpacesOnly(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	tend := buildBinary(t)
	attach := func() *attached {
		t.Helper()
		p, err := pty.Start(tend, []string{"attach", "-s", "companies"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
		if err != nil {
			t.Fatal(err)
		}
		a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
		go func() { _, _ = io.Copy(a, p) }()
		t.Cleanup(func() { _ = p.Close() })
		return a
	}
	t.Cleanup(func() { stopSession(t, "companies") })

	a := attach()
	a.waitForScreen(t, "the first space", func(s string) bool {
		return sidebarHas(s, "main") && sidebarHas(s, "spaces") && sidebarHas(s, "◇")
	})

	a.send(t, "\x02O")
	a.waitForScreen(t, "the panel", func(s string) bool {
		return strings.Contains(s, "COMPANIES") && strings.Contains(s, "all spaces") && strings.Contains(s, "1 space")
	})
	a.send(t, "n")
	a.waitForScreen(t, "the name asked for", func(s string) bool { return strings.Contains(s, "new company:") })
	a.send(t, "Acme Ltda\r")
	a.waitForScreen(t, "its spaces offered", func(s string) bool {
		return strings.Contains(s, "SPACES OF Acme Ltda") && strings.Contains(s, "[ ] main")
	})
	a.send(t, "\x1b")
	a.waitForScreen(t, "the list with it", func(s string) bool {
		return strings.Contains(s, "COMPANIES") && strings.Contains(s, "Acme Ltda") && strings.Contains(s, "0 spaces")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "the company chosen, its list empty", func(s string) bool {
		return !strings.Contains(s, "COMPANIES") && strings.Contains(s, "◆ Acme") && !sidebarHas(s, "main")
	})

	a.send(t, "\x02N")
	a.waitForScreen(t, "the new space in it", func(s string) bool {
		return sidebarHas(s, "space 2") && !sidebarHas(s, "main")
	})

	a.send(t, "\x02d")
	exited := make(chan error, 1)
	go func() { exited <- a.pty.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not exit after detaching")
	}
	a = attach()
	a.waitForScreen(t, "the company still chosen", func(s string) bool {
		return strings.Contains(s, "◆ Acme") && sidebarHas(s, "space 2") && !sidebarHas(s, "main")
	})

	a.send(t, "\x02O")
	a.waitForScreen(t, "the panel on the company", func(s string) bool {
		return strings.Contains(s, "COMPANIES") && strings.Contains(s, "1 space")
	})
	a.send(t, "\x1b[A\r")
	a.waitForScreen(t, "every space again", func(s string) bool {
		return !strings.Contains(s, "COMPANIES") && strings.Contains(s, "◇ all") && sidebarHas(s, "main") && sidebarHas(s, "space 2")
	})
}

// sidebarHas reports whether the sidebar's columns show text: the status
// line and the tab bar name the space being shown too, and that is not the
// list.
func sidebarHas(screen, text string) bool {
	lines := strings.Split(screen, "\n")
	for _, line := range lines[:max(len(lines)-1, 0)] {
		if r := []rune(line); len(r) > 0 && strings.Contains(string(r[:min(len(r), 32)]), text) {
			return true
		}
	}
	return false
}
