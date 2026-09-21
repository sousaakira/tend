package transport

import (
	"os"
	"path/filepath"
)

// StateDirEnv overrides where sessions are written down.
const StateDirEnv = "TEND_STATE_DIR"

// StatePath is the file a session's arrangement is kept in.
//
// It is not beside the socket. The runtime directory is for things that mean
// nothing once the machine restarts, and on most systems is wiped when it
// does; what a session looked like is exactly the thing worth having after
// that. So it goes where state goes: $XDG_STATE_HOME, or ~/.local/state.
//
// One exception: when the runtime directory has been pointed somewhere else,
// the state follows it. Somebody relocating the socket is sandboxing tend — a
// test, a second copy running beside the real one — and a sandbox that still
// writes into the real state directory is not one.
func StatePath(name string) (string, error) {
	if name == "" {
		name = DefaultSessionName
	}
	if err := validSessionName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// StateDir is where tend keeps what outlives a restart: sessions, and the
// machines a client keeps an eye on.
func StateDir() (string, error) { return stateDir() }

func stateDir() (string, error) {
	if dir := os.Getenv(StateDirEnv); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("TEND_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "state"), nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "tend"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "tend"), nil
}
