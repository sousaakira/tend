//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize subscribes ch to window-size changes. It reports whether this
// platform delivers them at all.
func notifyResize(ch chan<- os.Signal) bool {
	signal.Notify(ch, syscall.SIGWINCH)
	return true
}
