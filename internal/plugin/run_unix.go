//go:build unix

package plugin

import (
	"os/exec"
	"syscall"
)

// setGroup puts a plugin's command in a process group of its own, so that
// cancelling it can reach what it started.
//
// A manifest's command is usually a shell script, and killing the shell leaves
// its children running. Worse, a child that inherited the command's output
// pipe holds it open, and a Wait on the command then blocks until that child
// is done — a "sleep 30" hook survived the server's shutdown by exactly this
// route, holding Close open for its full half minute.
func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup ends the command and everything it started.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	// The group may be gone already, or the command may not lead one.
	return cmd.Process.Kill()
}
