//go:build unix

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	creack "github.com/creack/pty"
)

// Pty is a command running on a pseudo-terminal.
type Pty struct {
	f   *os.File
	cmd *exec.Cmd

	closeOnce sync.Once
	closeErr  error
}

func start(name string, args []string, opts Options) (*Pty, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env

	f, err := creack.StartWithSize(cmd, &creack.Winsize{
		Cols: opts.Size.Cols,
		Rows: opts.Size.Rows,
	})
	if err != nil {
		return nil, err
	}
	return &Pty{f: f, cmd: cmd}, nil
}

// Read returns what the process wrote to its terminal.
//
// When the child exits, Linux fails the next read on the master side with
// EIO rather than reporting end of file. Translating it here means callers
// can treat the PTY like any other reader instead of special-casing an errno
// that only one platform produces.
func (p *Pty) Read(b []byte) (int, error) {
	n, err := p.f.Read(b)
	if err != nil && isChildExited(err) {
		return n, io.EOF
	}
	return n, err
}

func isChildExited(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return errors.Is(pathErr.Err, syscall.EIO)
	}
	return errors.Is(err, syscall.EIO)
}

// Write sends input to the process, as though it had been typed.
func (p *Pty) Write(b []byte) (int, error) { return p.f.Write(b) }

// Resize tells the process its terminal changed size, which also delivers
// SIGWINCH so a full-screen application redraws.
func (p *Pty) Resize(size Size) error {
	if !size.Valid() {
		return nil
	}
	return creack.Setsize(p.f, &creack.Winsize{Cols: size.Cols, Rows: size.Rows})
}

// Pid is the process id, or 0 once it has been reaped.
func (p *Pty) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Wait blocks until the process exits and returns its exit error, if any.
func (p *Pty) Wait() error { return p.cmd.Wait() }

// Signal sends a signal to the process group.
//
// The group, not just the process: the child is a session leader, so its pid
// is also its process group id, and signalling the group reaches any helpers
// the agent started. Signalling only the leader would leave those behind.
func (p *Pty) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("pty: process not running")
	}
	if unixSig, ok := sig.(syscall.Signal); ok {
		if err := syscall.Kill(-p.cmd.Process.Pid, unixSig); err == nil {
			return nil
		}
		// The group may be gone already, or the child may not lead one.
		// Fall back to the process itself.
	}
	return p.cmd.Process.Signal(sig)
}

// Close stops the process and releases the terminal.
//
// It sends SIGHUP before closing the master, and the order matters more than
// it looks. Closing the master is not enough on its own: the descriptor is not
// registered with Go's poller, so a Read already blocked on it is a plain
// syscall that Close cannot interrupt, and the reading goroutine would stay
// parked forever. The signal is what actually ends the process, which ends the
// read.
//
// A process that ignores SIGHUP survives this. Callers that must bound
// shutdown follow up with Kill.
func (p *Pty) Close() error {
	p.closeOnce.Do(func() {
		_ = p.Signal(syscall.SIGHUP)
		p.closeErr = p.f.Close()
	})
	return p.closeErr
}

// Kill ends the process group immediately. It is the escalation for a process
// that did not answer SIGHUP.
func (p *Pty) Kill() error {
	return p.Signal(syscall.SIGKILL)
}
