//go:build !unix

package server

import "os/exec"

// On a platform without process groups only the command itself is ended.
func setCommandGroup(*exec.Cmd) {}

func killCommandGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
