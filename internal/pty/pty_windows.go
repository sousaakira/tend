//go:build windows

package pty

import "os"

// Pty is not implemented on Windows yet.
//
// Windows needs ConPTY, which is a different enough mechanism that wrapping it
// behind this interface is its own piece of work rather than a port of the
// Unix path. The seam is here so that work lands in one file; everything above
// this package is already platform-free.
type Pty struct{}

func start(string, []string, Options) (*Pty, error) { return nil, ErrUnsupported }

func (p *Pty) Read([]byte) (int, error)  { return 0, ErrUnsupported }
func (p *Pty) Write([]byte) (int, error) { return 0, ErrUnsupported }
func (p *Pty) Resize(Size) error         { return ErrUnsupported }
func (p *Pty) Pid() int                  { return 0 }
func (p *Pty) Wait() error               { return ErrUnsupported }
func (p *Pty) Signal(os.Signal) error    { return ErrUnsupported }
func (p *Pty) Kill() error               { return ErrUnsupported }
func (p *Pty) Close() error              { return nil }
