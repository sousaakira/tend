//go:build !unix

package plugin

import "os/exec"

// Process groups are a Unix idea. On other platforms the command is killed on
// its own, and a child it started outlives it.
func setGroup(*exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
