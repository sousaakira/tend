//go:build !linux

package pty

// Foreground has no implementation here yet.
//
// The terminal knows its foreground process group everywhere that has
// job control, but turning a process id into a name is per-platform: Linux
// reads /proc, the BSDs and macOS need sysctl. Returning nothing means
// detection falls back to the command the pane was started with, which is
// right for a pane opened as an agent and wrong only for one opened as a
// shell — a smaller gap than guessing.
func (p *Pty) Foreground() string { return "" }
