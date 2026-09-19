//go:build windows

package main

import "os/exec"

// detach has no equivalent here yet. Windows cannot run panes at all until the
// pty layer grows a ConPTY implementation, so starting a server in the
// background would only produce one with nothing in it.
func detach(*exec.Cmd) {}
