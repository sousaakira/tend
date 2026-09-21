//go:build unix

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	creack "github.com/creack/pty"
)

// Pty is a command running on a pseudo-terminal.
type Pty struct {
	f   *os.File
	pid int
	// cmd is set when this process started the command, and nil when the
	// terminal was handed over by another server. Only a parent can wait for
	// a child, so an adopted pty learns its process has gone some other way.
	cmd *exec.Cmd

	paused atomic.Bool

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
	managed, err := pollable(f)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = f.Close()
		return nil, err
	}
	return &Pty{f: managed, pid: cmd.Process.Pid, cmd: cmd}, nil
}

// pollable reopens a terminal's master so that Go's poller manages it.
//
// The library opens it blocking, which makes a Read on it a plain system call:
// nothing can interrupt it, not a deadline and not Close. That was survivable
// while the only way a read ended was the process dying, and is not once a
// reader has to be stopped on purpose — to hand the terminal to another server
// without losing what arrives in between. A descriptor that is non-blocking
// when it is wrapped is registered with the poller, and then a deadline works.
func pollable(f *os.File) (*os.File, error) {
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	syscall.CloseOnExec(fd)
	managed := os.NewFile(uintptr(fd), f.Name())
	_ = f.Close()
	return managed, nil
}

// Adopt takes over a terminal another server was running.
//
// The process in it is not this process's child, so there is nobody to wait
// for and no exit status to be had: it is known to have gone when its terminal
// reports end of file, and that is all that is known.
func Adopt(f *os.File, pid int) (*Pty, error) {
	if f == nil || pid <= 0 {
		return nil, errors.New("pty: nothing to adopt")
	}
	if err := syscall.SetNonblock(int(f.Fd()), true); err != nil {
		return nil, err
	}
	// Fd() above took the descriptor out of the poller's hands; wrapping a
	// duplicate puts one back in them.
	managed, err := pollable(f)
	if err != nil {
		return nil, err
	}
	return &Pty{f: managed, pid: pid}, nil
}

// Pause stops the reader: a Read in progress returns ErrPaused, and so does
// every Read after it until Resume.
//
// Nothing is lost by it. Output that arrives while paused stays in the kernel's
// buffer for whoever reads next, which is the point — that may be another
// server entirely.
func (p *Pty) Pause() {
	p.paused.Store(true)
	_ = p.f.SetReadDeadline(time.Now())
}

// Resume lets reading carry on after a Pause.
//
// Non-blocking mode is set again on the way. It belongs to the open terminal
// rather than to any one descriptor, so a duplicate handed to another process
// shares it — and starting a process with a file switches that file to
// blocking. After a handoff that was called off, this is what makes the next
// Pause work.
func (p *Pty) Resume() {
	p.paused.Store(false)
	_ = p.control(func(fd uintptr) error { return syscall.SetNonblock(int(fd), true) })
	_ = p.f.SetReadDeadline(time.Time{})
}

// Dup returns a second descriptor for the same terminal, for handing to
// another process. The caller owns it and closes it.
//
// Through SyscallConn rather than Fd, which would take the terminal out of the
// poller's hands and turn every later read into one nothing can interrupt.
func (p *Pty) Dup() (*os.File, error) {
	var dup int
	err := p.control(func(fd uintptr) error {
		var err error
		dup, err = syscall.Dup(int(fd))
		return err
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), p.f.Name()), nil
}

// control runs fn with the terminal's descriptor, held open for the duration.
func (p *Pty) control(fn func(fd uintptr) error) error {
	raw, err := p.f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := raw.Control(func(fd uintptr) { inner = fn(fd) }); err != nil {
		return err
	}
	return inner
}

// Release lets go of the terminal without touching the process in it.
//
// Close hangs the process up, which is right when a pane is being closed and
// exactly wrong when it has been handed to another server: the hangup would
// kill what the handoff exists to keep.
func (p *Pty) Release() error {
	p.closeOnce.Do(func() { p.closeErr = p.f.Close() })
	return p.closeErr
}

// Read returns what the process wrote to its terminal.
//
// When the child exits, Linux fails the next read on the master side with
// EIO rather than reporting end of file. Translating it here means callers
// can treat the PTY like any other reader instead of special-casing an errno
// that only one platform produces.
func (p *Pty) Read(b []byte) (int, error) {
	if p.paused.Load() {
		return 0, ErrPaused
	}
	n, err := p.f.Read(b)
	if err != nil && errors.Is(err, os.ErrDeadlineExceeded) && p.paused.Load() {
		return n, ErrPaused
	}
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
//
// The ioctl is made through control, not creack.Setsize: that one takes the
// descriptor with Fd(), which does not hold the file open while it is used:
// the race detector caught a resize reading it while the reader goroutine,
// its pane just exited, was closing it. Dup and Foreground borrow it the same
// way for the same reason.
func (p *Pty) Resize(size Size) error {
	if !size.Valid() {
		return nil
	}
	ws := creack.Winsize{Rows: size.Rows, Cols: size.Cols}
	return p.control(func(fd uintptr) error {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws))); errno != 0 {
			return errno
		}
		return nil
	})
}

// Pid is the process id.
func (p *Pty) Pid() int { return p.pid }

// Wait blocks until the process exits and returns its exit error, if any.
//
// For an adopted terminal there is no exit error to return. Its process was
// somebody else's child, so all that can be done is to see it gone — and that
// is bounded, because a process that closed its terminal and carried on is
// allowed to, and must not hold a pane open forever.
func (p *Pty) Wait() error {
	if p.cmd != nil {
		return p.cmd.Wait()
	}
	deadline := time.Now().Add(adoptedExitGrace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(p.pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// adoptedExitGrace is how long Wait watches for an adopted process to go.
const adoptedExitGrace = 2 * time.Second

// Signal sends a signal to the process group.
//
// The group, not just the process: the child is a session leader, so its pid
// is also its process group id, and signalling the group reaches any helpers
// the agent started. Signalling only the leader would leave those behind.
func (p *Pty) Signal(sig os.Signal) error {
	unixSig, ok := sig.(syscall.Signal)
	if !ok || p.pid <= 0 {
		return errors.New("pty: process not running")
	}
	if err := syscall.Kill(-p.pid, unixSig); err == nil {
		return nil
	}
	// The group may be gone already, or the child may not lead one. Fall back
	// to the process itself.
	return syscall.Kill(p.pid, unixSig)
}

// Close stops the process and releases the terminal.
//
// It sends SIGHUP before closing the master. The signal is what ends the
// process; closing the master on its own would leave a program that ignores
// its terminal going away running with nothing attached to it.
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
