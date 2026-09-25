//go:build unix

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// TestTheErrorsPanelConnectsListsAndFixes: with no server connected the
// errors panel offers to connect one; a token the server refuses is said in
// the box and not kept; one it takes is written to the settings file,
// readable by its owner alone, and the errors are listed; enter opens one
// with its stack; r resolves it on the server; and f types it, with its
// stack and what to do, into the pane the panel was opened from, submitting
// nothing. GlitchTip is a stand-in answering as it does. If it regresses,
// the key cannot be given, a wrong one is kept, or an error never reaches
// the agent.
func TestTheErrorsPanelConnectsListsAndFixes(t *testing.T) {
	var mu sync.Mutex
	var changed []string
	gt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer right-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"shop","name":"Shop"}]`)
		case p == "/api/0/organizations/shop/projects/":
			_, _ = io.WriteString(w, `[{"slug":"api","name":"api"}]`)
		case p == "/api/0/organizations/shop/issues/":
			_, _ = io.WriteString(w, `[{"id":"75","shortId":"API-26","title":"KnexTimeoutError: pool full","level":"error","status":"unresolved","count":"1543","userCount":2,"firstSeen":"2026-08-01T10:00:00Z","lastSeen":"2026-09-20T12:00:00Z","project":{"slug":"api"}}]`)
		case p == "/api/0/issues/75/events/latest/":
			_, _ = io.WriteString(w, `{"eventID":"e1","entries":[{"type":"exception","data":{"values":[{"type":"KnexTimeoutError","value":"pool full","stacktrace":{"frames":[
				{"absPath":"/app/node_modules/knex/client.js","lineNo":465,"function":"acquire","inApp":false},
				{"absPath":"/app/src/models/Score.js","lineNo":144,"function":"complement","inApp":true,"context_line":"await pg('t').first();"}]}}]}}]}`)
		case p == "/api/0/issues/75/" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			changed = append(changed, body["status"])
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gt.Close()

	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "TEND_GLITCHTIP_TOKEN=")
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "errors"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "errors") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02E")
	a.waitForScreen(t, "the offer to connect", func(s string) bool { return strings.Contains(s, "GlitchTip is not connected") })
	a.send(t, "\r")
	a.waitForScreen(t, "the connect box", func(s string) bool { return strings.Contains(s, "connect GlitchTip") })
	a.send(t, "\x15"+gt.URL+"\twrong-token\r")
	a.waitForScreen(t, "the token refused", func(s string) bool { return strings.Contains(s, "GlitchTip refused the token") })
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "wrong-token") {
		t.Fatal("a refused token was kept")
	}
	a.send(t, "\x15right-token\r")
	a.waitForScreen(t, "the errors", func(s string) bool {
		return strings.Contains(s, "API-26") && strings.Contains(s, "KnexTimeoutError: pool full") && strings.Contains(s, "1543")
	})
	b, _ := os.ReadFile(cfg)
	st, _ := os.Stat(cfg)
	if !strings.Contains(string(b), `token = "right-token"`) || !strings.Contains(string(b), `url = "`+gt.URL+`"`) || st.Mode().Perm() != 0o600 {
		t.Errorf("settings (%v):\n%s", st.Mode(), b)
	}

	// Resolved from the list, without opening it.
	a.send(t, "\x18")
	a.waitForScreen(t, "resolved from the list", func(s string) bool { return strings.Contains(s, "API-26 is resolved") })

	a.send(t, "\r")
	a.waitForScreen(t, "the error's stack", func(s string) bool {
		return strings.Contains(s, "▸ at complement") && strings.Contains(s, "await pg('t').first();")
	})
	a.send(t, "r")
	a.waitForScreen(t, "resolved", func(s string) bool { return strings.Contains(s, "API-26 is resolved") })
	mu.Lock()
	got := strings.Join(changed, ",")
	mu.Unlock()
	if got != "resolved,resolved" {
		t.Errorf("changed: %q", got)
	}

	a.send(t, "f")
	a.waitForScreen(t, "the error typed into the pane", func(s string) bool {
		return !strings.Contains(s, "ERRORS ·") && strings.Contains(s, "Fix this error reported by GlitchTip (API-26") &&
			strings.Contains(s, "typed into the pane")
	})
}
