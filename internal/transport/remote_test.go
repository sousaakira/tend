package transport

import (
	"strings"
	"testing"
)

// TestTheFarSideRunsTheTendAnAttachInstalled: over plain ssh the far side
// runs ~/.local/bin/tend when it is there, since an attach puts tend there
// and a non-interactive ssh often has no ~/.local/bin on its PATH; found on
// a real host, where every -ssh command then failed with "tend: command not
// found". A path an attach settled on wins, and a TEND_SSH wrapper gets the
// plain word.
func TestTheFarSideRunsTheTendAnAttachInstalled(t *testing.T) {
	t.Setenv(RemoteCommandEnv, "")
	argv := RemoteArgv("host", "s")
	if argv[0] != "ssh" || !strings.Contains(argv[3], "$HOME/.local/bin/tend") || argv[4] != "bridge" {
		t.Errorf("argv = %q", argv)
	}
	RemoteTend = "'/opt/tend'"
	defer func() { RemoteTend = "" }()
	if argv := RemoteArgv("host", "s"); argv[3] != "'/opt/tend'" {
		t.Errorf("a settled path should win: %q", argv)
	}
	RemoteTend = ""
	t.Setenv(RemoteCommandEnv, "fake-ssh")
	if argv := RemoteArgv("host", "s"); argv[2] != "tend" {
		t.Errorf("a wrapper should get the plain word: %q", argv)
	}
}
