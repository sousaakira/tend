//go:build unix

package server

import (
	"os/exec"
	"syscall"
)

// setCommandGroup gives a tab bar command a process group of its own, so a
// timeout reaches what the shell started and not only the shell.
func setCommandGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killCommandGroup ends the command and everything it started.
func killCommandGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
