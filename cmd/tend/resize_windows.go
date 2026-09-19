//go:build windows

package main

import "os"

// notifyResize has nothing to subscribe to: Windows reports console resizes
// through the input record stream rather than a signal, which the client will
// read once it exists.
func notifyResize(chan<- os.Signal) bool { return false }
