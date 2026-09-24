package notify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A sound is for the case the notification cannot cover: the terminal is
// behind another window, or the person is not looking at the screen at all.
//
// herdr bundles two mp3s and shells out to a player. tend does not carry audio
// files — it has no bundled assets and would need a reason to start — so a
// sound here is a file the user names, else the desktop's own sound theme
// (freedesktop's complete and message-new-instant, macOS's Glass and Ping),
// else the terminal's bell. The theme came second when the bell turned out
// to be silence: GNOME Terminal, the owner's, rings it as the desktop's
// alert sound, which is off unless somebody turned it on, so a finished
// agent made no sound at all. "bell" names the bell, for who wants it.

// Sound names what happened, since the two deserve different sounds.
type Sound uint8

const (
	// SoundDone is an agent that finished.
	SoundDone Sound = iota
	// SoundRequest is an agent that is waiting to be answered.
	SoundRequest
	// SoundNone is silence: a change the settings mute.
	SoundNone
)

// players are tried in order for a file the user named. Each is asked to play
// and be quiet about it; the first that exists wins.
var players = []struct {
	name string
	args []string
}{
	{"paplay", nil},
	{"pw-play", nil},
	{"aplay", []string{"-q"}},
	{"afplay", nil},
	{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet"}},
	{"play", []string{"-q"}},
}

// playTimeout bounds a player. A notification sound is a second or two; one
// still going after this is a player waiting for something.
const playTimeout = 10 * time.Second

// Player plays notification sounds.
type Player struct {
	// Enabled is whether to make any sound at all.
	Enabled bool
	// Done and Request are files to play, or empty for the bell.
	Done    string
	Request string
	// Bell writes the terminal's bell. It is a function so the client can
	// send it down the same stream it draws on.
	Bell func()

	once sync.Once
	tool string
	args []string
}

// Play makes the sound for what happened. It returns at once: playing is
// somebody else's process, and the session must not wait for it.
func (p *Player) Play(sound Sound) {
	if p == nil || !p.Enabled || sound == SoundNone {
		return
	}
	path := p.Done
	if sound == SoundRequest {
		path = p.Request
	}
	if path == "" {
		path = themeSound(sound)
	}
	if path == "" || path == "bell" {
		if p.Bell != nil {
			p.Bell()
		}
		return
	}

	p.once.Do(func() {
		for _, candidate := range players {
			if found, err := exec.LookPath(candidate.name); err == nil {
				p.tool, p.args = found, candidate.args
				return
			}
		}
	})
	if p.tool == "" || (filepath.Base(p.tool) == "aplay" && !strings.HasSuffix(path, ".wav")) {
		// Nothing to play it with — aplay reads only wav, and the theme's
		// are ogg. The bell is better than silence, and says the same
		// thing.
		if p.Bell != nil {
			p.Bell()
		}
		return
	}

	cmd := exec.Command(p.tool, append(append([]string{}, p.args...), path)...)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		timer := time.AfterFunc(playTimeout, func() { _ = cmd.Process.Kill() })
		defer timer.Stop()
		_ = cmd.Wait()
	}()
}

// themeNames are the desktop sound theme's names for each sound, the first
// found played: freedesktop's (every Linux desktop's, in
// /usr/share/sounds/freedesktop/stereo), then macOS's system sounds.
var themeNames = map[Sound][]string{
	SoundDone:    {"freedesktop/stereo/complete", "freedesktop/stereo/message", "/System/Library/Sounds/Glass"},
	SoundRequest: {"freedesktop/stereo/message-new-instant", "freedesktop/stereo/dialog-question", "freedesktop/stereo/bell", "/System/Library/Sounds/Ping"},
}

// themeSound is the theme's file for a sound, or "" when there is none.
func themeSound(sound Sound) string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "sounds"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for _, d := range filepath.SplitList(data) {
		dirs = append(dirs, filepath.Join(d, "sounds"))
	}
	for _, name := range themeNames[sound] {
		candidates := []string{}
		if filepath.IsAbs(name) {
			candidates = append(candidates, name+".aiff")
		} else {
			for _, d := range dirs {
				for _, ext := range []string{".oga", ".ogg", ".wav"} {
					candidates = append(candidates, filepath.Join(d, name+ext))
				}
			}
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c
			}
		}
	}
	return ""
}
