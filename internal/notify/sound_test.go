//go:build unix

package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWithNoFileNamedTheDesktopsSoundPlays: a sound with no file named plays
// the desktop's sound theme — complete for a finished agent, the instant
// message for one waiting — through a player, and rings the bell only when
// "bell" is named. If it regresses, a finished agent makes no sound in a
// terminal whose bell is silent, GNOME Terminal's by default.
func TestWithNoFileNamedTheDesktopsSoundPlays(t *testing.T) {
	data := t.TempDir()
	stereo := filepath.Join(data, "sounds", "freedesktop", "stereo")
	if err := os.MkdirAll(stereo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"complete.oga", "message-new-instant.oga"} {
		if err := os.WriteFile(filepath.Join(stereo, name), []byte("ogg"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	played := filepath.Join(bin, "played")
	script := "#!/bin/sh\necho \"$1\" >> " + played + "\n"
	if err := os.WriteFile(filepath.Join(bin, "paplay"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("XDG_DATA_DIRS", data)
	t.Setenv("HOME", t.TempDir())

	rang := 0
	p := &Player{Enabled: true, Bell: func() { rang++ }}
	p.Play(SoundDone)
	p.Play(SoundRequest)
	// The two play at once, so in either order.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := os.ReadFile(played)
		if strings.Count(string(got), "\n") >= 2 {
			if !strings.Contains(string(got), filepath.Join(stereo, "complete.oga")) || !strings.Contains(string(got), filepath.Join(stereo, "message-new-instant.oga")) {
				t.Errorf("played %q", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("played %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rang != 0 {
		t.Errorf("the bell rang %d times with a theme to play", rang)
	}

	bell := &Player{Enabled: true, Done: "bell", Bell: func() { rang++ }}
	bell.Play(SoundDone)
	if rang != 1 {
		t.Errorf(`"bell" rang %d times`, rang)
	}
}
