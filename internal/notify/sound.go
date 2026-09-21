package notify

import (
	"os/exec"
	"sync"
	"time"
)

// A sound is for the case the notification cannot cover: the terminal is
// behind another window, or the person is not looking at the screen at all.
//
// herdr bundles two mp3s and shells out to a player. tend does not carry audio
// files — it has no bundled assets and would need a reason to start — so a
// sound here is a file the user names, and the terminal's bell when they name
// none. The bell is not much, and it is the one thing every terminal has.

// Sound names what happened, since the two deserve different sounds.
type Sound uint8

const (
	// SoundDone is an agent that finished.
	SoundDone Sound = iota
	// SoundRequest is an agent that is waiting to be answered.
	SoundRequest
)

// players are tried in order for a file the user named. Each is asked to play
// and be quiet about it; the first that exists wins.
var players = []struct {
	name string
	args []string
}{
	{"paplay", nil},
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
	if p == nil || !p.Enabled {
		return
	}
	path := p.Done
	if sound == SoundRequest {
		path = p.Request
	}
	if path == "" {
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
	if p.tool == "" {
		// Nothing to play it with. The bell is better than silence, and says
		// the same thing.
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
