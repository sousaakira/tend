package main

import (
	"errors"

	"github.com/sousaakira/tend/internal/proto"
)

// A tend server outlives the client that started it. Building a new binary and
// running it therefore leaves the old server answering, and everything works
// until the first thing the new client knows about and the old one does not.
//
// The client can see this at the handshake, so it says so there rather than
// letting the user find out when an action fails. A notice alone was not
// enough: it names a command in another terminal, which is one step further
// than most people will go while something is already in front of them. The
// restart is offered where the problem is, and taken only when asked for.

// staleServerOverlay is what the notice says, with the keys that answer it.
func staleServerOverlay(build, self, session string) []string {
	if build == "" {
		// Old enough that it predates the field naming the build, which is
		// itself the answer to "how old".
		build = "an older build"
	}
	return []string{
		"This session runs an older server",
		"",
		"server:  " + build,
		"client:  " + self,
		"",
		"Anything this client can do that the old server",
		"cannot will fail until it is replaced.",
		"",
		"r      restart it now, closing its panes",
		"enter  keep it and carry on",
		"",
		"Elsewhere: " + restartCommand(session),
	}
}

// warnIfServerIsOlder puts the notice up when the session is being run by a
// binary other than this one.
func (t *tui) warnIfServerIsOlder() {
	build := t.client.Server().Build
	if build == version {
		return
	}

	t.mu.Lock()
	t.staleServer = true
	t.overlay = staleServerOverlay(build, version, t.session)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// staleServerKeys answers the notice. It reports whether it took the input.
func (t *tui) staleServerKeys(data []byte) (bool, error) {
	t.mu.Lock()
	up := t.staleServer
	t.mu.Unlock()
	if !up {
		return false, nil
	}

	for _, key := range splitKeys(data) {
		t.dismissStaleServer()
		if key == "r" || key == "R" {
			return true, t.restartServer()
		}
		// Every other key means "carry on", and is not passed through: the
		// notice is over the screen, so a keystroke aimed past it was aimed
		// at something the user could not see.
		return true, nil
	}
	return true, nil
}

func (t *tui) dismissStaleServer() {
	t.mu.Lock()
	t.staleServer = false
	t.overlay = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// restartServer stops the session's server so a new one takes its place.
//
// Nothing here starts the replacement. Shutting the old one down drops the
// connection, which is the same thing as a server dying for any other reason,
// and the reconnect that follows starts a fresh one exactly as the first
// attach did. One path, already the one that has to work.
func (t *tui) restartServer() error {
	t.setMessage("restarting the server", false)
	if err := t.client.Shutdown(); err != nil && !errors.Is(err, proto.ErrUnknownMethod) {
		return err
	}
	return nil
}

// staleServerCommand is the key binding that brings the notice back, for
// somebody who dismissed it and then hit the failure it was about.
func (t *tui) showStaleServerNotice() {
	build := t.client.Server().Build
	t.mu.Lock()
	t.staleServer = true
	t.overlay = staleServerOverlay(build, version, t.session)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// reportStaleServer turns "the server has never heard of that" into the notice
// explaining why, and reports whether it handled the error.
//
// It is not fatal. The action did not happen, but everything else about the
// session still works, and closing the client over it would lose the user's
// place for a reason that had nothing to do with them.
func (t *tui) reportStaleServer(err error) bool {
	if !errors.Is(err, proto.ErrUnknownMethod) {
		return false
	}
	t.showStaleServerNotice()
	return true
}

// staleServerUp reports whether the notice has the keyboard.
func (t *tui) staleServerUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.staleServer
}
