//go:build linux

package pty

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Foreground returns the name of the program currently in charge of the
// terminal, which is not the same as the command the pane was started with.
//
// Almost nobody opens a pane by naming an agent. They open a shell and type
// its name, so the pane's command is "zsh" forever while the thing on screen
// is Claude Code. The terminal itself knows which process group is in the
// foreground — that is how it decides where Ctrl+C goes — and asking it is
// the only answer that stays right as the user starts and quits programs.
//
// An empty name is not an error: the process may have just exited, or be one
// this process is not allowed to look at.
func (p *Pty) Foreground() string {
	if p.f == nil {
		return ""
	}
	pgrp, err := foregroundGroup(p.f)
	if err != nil || pgrp <= 0 {
		return ""
	}
	name, err := os.ReadFile("/proc/" + strconv.Itoa(pgrp) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(name))
}

// foregroundGroup asks the terminal which process group has it.
//
// The descriptor is borrowed through SyscallConn rather than taken with Fd().
// This runs on a timer while another goroutine is reading the same pty, and
// Fd() neither holds the file open for the duration nor leaves the descriptor
// as it found it — it detaches it from the runtime poller, which would turn
// every later read on it into a blocking one.
func foregroundGroup(f *os.File) (int, error) {
	raw, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}

	var pgrp int32
	var errno syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(
			syscall.SYS_IOCTL,
			fd,
			uintptr(syscall.TIOCGPGRP),
			uintptr(unsafe.Pointer(&pgrp)),
		)
	}); err != nil {
		return 0, err
	}
	if errno != 0 {
		return 0, errno
	}
	return int(pgrp), nil
}
