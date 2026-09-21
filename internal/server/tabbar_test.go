//go:build unix

package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/config"
)

func statusTexts(s *Server) []string {
	segs, _ := s.tabBarSnapshot()
	var out []string
	for _, seg := range segs {
		if seg.Zoom {
			out = append(out, "<zoom>")
			continue
		}
		out = append(out, seg.Text)
	}
	return out
}

// TestTabBarEntriesAreWorkedOutByTheServer: text and hostname are there at
// once, a command's last line arrives once it has run and follows its output
// on the interval, and zoom is left as a slot for the client. If it regresses,
// the bar shows nothing, or a command's first line, or a stale value.
func TestTabBarEntriesAreWorkedOutByTheServer(t *testing.T) {
	s := newServer(t)
	counter := filepath.Join(t.TempDir(), "n")
	s.ConfigureTabBar([]config.TabBarEntry{
		{Type: "zoom"},
		{Type: "text", Text: "prod"},
		{Type: "hostname"},
		{Type: "command", IntervalSeconds: 1,
			Command: "echo first; n=$(cat " + counter + " 2>/dev/null || echo 0); n=$((n+1)); echo $n > " + counter + "; printf '\\033[31mrun %s\\033[0m\\n' $n"},
	}, " | ")

	host, _ := os.Hostname()
	waitFor(t, "the command's first run", func() bool {
		got := statusTexts(s)
		return len(got) == 4 && got[3] == "run 1"
	})
	got := statusTexts(s)
	if got[0] != "<zoom>" || got[1] != "prod" || got[2] != host {
		t.Errorf("entries = %q", got)
	}
	if _, sep := s.tabBarSnapshot(); sep != " | " {
		t.Errorf("separator = %q", sep)
	}
	waitFor(t, "the command's second run", func() bool {
		got := statusTexts(s)
		return len(got) == 4 && got[3] == "run 2"
	})
}

// TestATabBarCommandThatHangsIsKilled: a status command stuck on the network
// must not pile up one process per interval, nor leave the entry claiming a
// value it no longer has.
func TestATabBarCommandThatHangsIsKilled(t *testing.T) {
	s := newServer(t)
	marker := filepath.Join(t.TempDir(), "child")
	s.ConfigureTabBar([]config.TabBarEntry{
		// The child writes its pid and sleeps; the shell waits on it. Killing
		// only the shell would leave the sleep running.
		{Type: "command", TimeoutSeconds: 1, IntervalSeconds: 60,
			Command: "sh -c 'echo $$ > " + marker + "; exec sleep 30' ; echo never"},
	}, " ")

	var pid string
	waitFor(t, "the child to start", func() bool {
		data, err := os.ReadFile(marker)
		pid = strings.TrimSpace(string(data))
		return err == nil && pid != ""
	})
	n, err := strconv.Atoi(pid)
	if err != nil {
		t.Fatalf("pid %q: %v", pid, err)
	}
	waitFor(t, "the child to be killed with its group", func() bool {
		return syscall.Kill(n, 0) == syscall.ESRCH
	})
	if got := statusTexts(s); len(got) != 0 {
		t.Errorf("a command that timed out still shows %q", got)
	}
}

// TestReconfiguringTheTabBarStopsTheOldCommands: a reload that changes the
// bar must not leave the previous commands running beside the new ones.
func TestReconfiguringTheTabBarStopsTheOldCommands(t *testing.T) {
	s := newServer(t)
	log := filepath.Join(t.TempDir(), "log")
	s.ConfigureTabBar([]config.TabBarEntry{
		{Type: "command", IntervalSeconds: 1, Command: "echo tick >> " + log + "; echo old"},
	}, " ")
	waitFor(t, "the old command", func() bool {
		got := statusTexts(s)
		return len(got) == 1 && got[0] == "old"
	})
	s.ConfigureTabBar([]config.TabBarEntry{{Type: "text", Text: "new"}}, " ")
	before, _ := os.ReadFile(log)
	time.Sleep(1500 * time.Millisecond)
	after, _ := os.ReadFile(log)
	if len(after) != len(before) {
		t.Error("the replaced command kept running")
	}
	if got := statusTexts(s); len(got) != 1 || got[0] != "new" {
		t.Errorf("entries = %q, want only the new one", got)
	}
}

// TestStatusCommandOutputIsCleaned: a colourised or chatty command shows as
// one line of text, never as escapes that reach the client's terminal.
func TestStatusCommandOutputIsCleaned(t *testing.T) {
	for in, want := range map[string]string{
		"old\nfinal\n":                  "final",
		"no newline":                    "no newline",
		"\x1b[1;32mgreen\x1b[0m\n":      "green",
		"\x1b]0;title\x07after":         "after",
		"a\x1bPdcs\x1b\\b":              "ab",
		"  spaced\t​out  ":              "spacedout",
		strings.Repeat("x", 200) + "\n": strings.Repeat("x", maxStatusText),
	} {
		if got := statusText(string(stripControlSequences(lastLine([]byte(in))))); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
