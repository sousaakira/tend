//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach arranges for the server to survive this process.
//
// Setsid puts it in its own session, so it is not in our process group and
// does not receive the Ctrl+C meant for the client, and it has no controlling
// terminal to be hung up when the terminal that started it closes.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
