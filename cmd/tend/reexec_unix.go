//go:build unix

package main

import "syscall"

// reexec replaces this process with the binary at path, keeping its pid,
// its terminal and everything open on it: to the pane it runs in, nothing
// ended.
func reexec(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}
