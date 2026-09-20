package transport

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// A session on another machine is reached by running tend there and talking to
// it through the command that started it. Over ssh that means no port to open,
// no second authentication scheme and nothing listening on the network: the
// socket stays private to its machine, and ssh is the only door in.
//
// The remote end is `tend bridge`, which does nothing but join its stdin and
// stdout to the session's socket. Everything else is the same protocol over a
// different pipe, so a remote session is not a second kind of session.

// RemoteCommandEnv overrides the program used to reach another machine. It
// defaults to ssh; setting it lets a test stand in a local shell, and lets a
// user whose ssh is wrapped in something else say so.
const RemoteCommandEnv = "TEND_SSH"

// stderrLimit bounds how much of the remote side's complaint is kept. It is
// kept at all because "connection closed" says nothing, and the reason — tend
// not installed there, a host key refused — is on stderr.
const stderrLimit = 8 * 1024

// remoteCloseGrace is how long the remote command is given to end once its
// input is closed.
const remoteCloseGrace = 2 * time.Second

// RemoteArgv is the command that carries a session from host.
func RemoteArgv(host, session string) []string {
	program := os.Getenv(RemoteCommandEnv)
	if program == "" {
		// -T: no terminal on the far side. The protocol is binary, and a pty
		// in the middle would translate line endings into it.
		return []string{"ssh", "-T", host, "tend", "bridge", "-s", session}
	}
	return append(strings.Fields(program), host, "tend", "bridge", "-s", session)
}

// Remote starts a command and returns its stdio as a connection.
func Remote(argv []string) (*RemoteConn, error) {
	if len(argv) == 0 {
		return nil, errors.New("transport: no remote command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	rc := &RemoteConn{cmd: cmd, stdin: stdin, stdout: stdout}
	cmd.Stderr = &rc.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("transport: starting %s: %w", argv[0], err)
	}
	return rc, nil
}

// RemoteConn is a connection made of a running command's stdin and stdout.
type RemoteConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr limitedBuffer

	closeOnce sync.Once
	closeErr  error
}

func (c *RemoteConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *RemoteConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

// Complaint is what the command said on stderr, trimmed.
func (c *RemoteConn) Complaint() string { return strings.TrimSpace(c.stderr.String()) }

// Close ends the command. Closing its input is the polite request; it is
// killed if it has not gone once the grace period runs out, so a hung ssh does
// not hold the client open.
func (c *RemoteConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		done := make(chan struct{})
		go func() {
			_ = c.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(remoteCloseGrace):
			_ = c.cmd.Process.Kill()
			<-done
		}
		_ = c.stdout.Close()
	})
	return c.closeErr
}

// limitedBuffer keeps the first stderrLimit bytes written to it and drops the
// rest, so a remote side that will not stop talking cannot grow this forever.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := stderrLimit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
