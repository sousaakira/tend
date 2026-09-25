//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// TestTheBrowserPromptOffersTheLastPages: prefix+B lists the pages last
// opened, the last first, five at most; the arrows put one in the field for
// enter, and a click opens one at once; an address longer than a name opens
// whole, and one pasted over the prompt's "https://" is not doubled; and
// the list is there again after the client restarts. The browser
// is a script saying what it was given. If it regresses, a page opened a
// minute ago has to be typed again, or a long address opens cut short.
func TestTheBrowserPromptOffersTheLastPages(t *testing.T) {
	bin := t.TempDir()
	opened := filepath.Join(bin, "opened")
	browser := filepath.Join(bin, "browser")
	if err := os.WriteFile(browser, []byte("#!/bin/sh\necho \"$1\" >> "+opened+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings+"\n[browser]\ncommand = \""+browser+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	tend := buildBinary(t)
	attach := func() *attached {
		p, err := pty.Start(tend, []string{"attach", "-s", "pages"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
		if err != nil {
			t.Fatal(err)
		}
		a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
		go func() { _, _ = io.Copy(a, p) }()
		t.Cleanup(func() { _ = p.Close() })
		a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
		return a
	}
	t.Cleanup(func() { stopSession(t, "pages") })
	a := attach()

	long := "https://example.com/a/very/long/path/that/goes/well/past/sixty/four/characters?with=a&query=string"
	pages := []string{"https://one.example/", "https://two.example/", long}
	for _, page := range pages {
		a.send(t, "\x02B")
		a.waitForScreen(t, "the prompt", func(s string) bool { return strings.Contains(s, "open in browser") })
		// Pasted whole, over the "https://" the prompt starts with.
		a.send(t, page+"\r")
		waitForFileContent(t, opened, page+"\n")
	}
	if got, _ := os.ReadFile(opened); string(got) != strings.Join(pages, "\n")+"\n" {
		t.Fatalf("opened: %q", got)
	}

	a.send(t, "\x02B")
	a.waitForScreen(t, "the last pages, the last first", func(s string) bool {
		return strings.Contains(s, "recent") && strings.Contains(s, "1 "+long[:40]) &&
			strings.Contains(s, "2 https://two.example/") && strings.Contains(s, "3 https://one.example/")
	})
	a.send(t, "\x1b[B\x1b[B\x1b[B") // to the third: one.example
	a.send(t, "\r")
	a.waitForScreen(t, "the prompt gone", func(s string) bool { return !strings.Contains(s, "open in browser") })
	if got, _ := os.ReadFile(opened); !strings.HasSuffix(string(got), "https://one.example/\n") {
		t.Fatalf("the arrows opened: %q", got)
	}

	// After a restart of the client, a click on the second opens it.
	_ = a.pty.Close()
	a = attach()
	a.send(t, "\x02B")
	a.waitForScreen(t, "the list again", func(s string) bool { return strings.Contains(s, "2 https://") })
	row, col := -1, -1
	for y, line := range a.lines() {
		if c := columnOfString(line, "2 https://"); c >= 0 {
			row, col = y, c
		}
	}
	a.clickAt(t, col+4, row+1)
	a.waitForScreen(t, "the prompt gone", func(s string) bool { return !strings.Contains(s, "open in browser") })
	// The list was one.example (last opened), the long one, two.example.
	waitForFileContent(t, opened, long+"\n"+"https://one.example/\n"+long+"\n")
}
