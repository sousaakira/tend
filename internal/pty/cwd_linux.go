//go:build linux

package pty

import (
	"os"
	"strconv"
)

// Cwd returns the directory the pane's process is in now, or "" when it cannot
// be told. It is what a restored pane is started in, so that coming back means
// coming back to where the work was rather than to where the shell was opened.
func (p *Pty) Cwd() string {
	pid := p.Pid()
	if pid <= 0 {
		return ""
	}
	dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return dir
}
