// Package pty runs a command on a pseudo-terminal.
//
// A pane's process must believe it owns a terminal: that is what makes an
// agent draw its spinner, report a title and answer cursor queries at all.
// Running it on a pipe would change its behaviour and silently defeat
// detection.
//
// The OS-specific work lives in pty_unix.go and pty_windows.go; everything
// here is shared and platform-free.
package pty

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned when this build has no PTY implementation.
var ErrUnsupported = errors.New("pty: not supported on this platform")

// Size is a terminal's dimensions in character cells.
type Size struct {
	Cols uint16
	Rows uint16
}

// Valid reports whether the size is usable. A zero dimension makes most
// applications either refuse to draw or draw into nothing.
func (s Size) Valid() bool { return s.Cols > 0 && s.Rows > 0 }

func (s Size) String() string { return fmt.Sprintf("%dx%d", s.Cols, s.Rows) }

// DefaultSize is used when a caller has no terminal to take dimensions from,
// such as when output is redirected.
var DefaultSize = Size{Cols: 120, Rows: 40}

// Options configures a command started on a PTY.
type Options struct {
	// Dir is the working directory. Empty means the current one.
	Dir string
	// Env is the environment. Nil inherits the parent's, which is normally
	// what an agent needs — it carries TERM, PATH and the credentials the
	// agent authenticates with.
	Env []string
	// Size is the initial terminal size. An invalid size falls back to
	// DefaultSize rather than failing, since a wrong size is recoverable and
	// a dead pane is not.
	Size Size
}

// Start runs name with args on a new pseudo-terminal.
//
// The returned Pty is the controlling side: reading it yields what the process
// wrote to its terminal, and writing to it is indistinguishable from typing.
func Start(name string, args []string, opts Options) (*Pty, error) {
	if name == "" {
		return nil, errors.New("pty: no command")
	}
	if !opts.Size.Valid() {
		opts.Size = DefaultSize
	}
	return start(name, args, opts)
}
