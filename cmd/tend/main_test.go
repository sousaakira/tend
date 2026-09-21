package main

import (
	"strings"
	"testing"
)

// TestHerdrsLaunchFlagsAttach: `tend --remote host [--session name]`, as
// herdr is started, attaches there; each flag also takes =value; and with a
// command they are refused, as herdr refuses them. If it regresses, the way
// the owner starts a remote session prints an unknown-command error.
func TestHerdrsLaunchFlagsAttach(t *testing.T) {
	for in, want := range map[string]string{
		"":                                  "attach",
		"--remote root@10.8.0.110":          "attach -ssh root@10.8.0.110",
		"--remote=root@host --session=work": "attach -ssh root@host -s work",
		"--session work --remote h":         "attach -s work -ssh h",
		"ls -s x":                           "ls -s x",
	} {
		got, err := launchArgs(strings.Fields(in))
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("launchArgs(%q) = %q, %v; want %q", in, strings.Join(got, " "), err, want)
		}
	}
	for _, bad := range []string{"--remote", "--remote --session x", "--remote h ls", "--session="} {
		if _, err := launchArgs(strings.Fields(bad)); err == nil {
			t.Errorf("launchArgs(%q) should be refused", bad)
		}
	}
}
