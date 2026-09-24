//go:build linux

package pty

import (
	"os"
	"path/filepath"
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

// FollowCwd is where a new terminal opened from this pane should start:
// herdr's follow_cwd, the directory of the program in charge of the
// terminal now — the agent, the editor — and the shell's when that cannot
// be read. The two differ when the program was started with a directory of
// its own (`claude` launched from a script, `cd x && nvim` in a subshell):
// the one being worked in is the program's, not the prompt's.
func (p *Pty) FollowCwd() string {
	if p.f != nil {
		if pgrp, err := foregroundGroup(p.f); err == nil && pgrp > 0 {
			if dir, err := os.Readlink("/proc/" + strconv.Itoa(pgrp) + "/cwd"); err == nil && filepath.IsAbs(dir) {
				return dir
			}
		}
	}
	return p.Cwd()
}
