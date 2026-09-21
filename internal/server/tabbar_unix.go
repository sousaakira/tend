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

// detach starts a background command in a session of its own, so it neither
// receives the server's signals nor holds its terminal: a command bound to a
// key must be free to outlive the server that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
