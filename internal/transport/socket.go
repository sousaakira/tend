// Package transport carries protocol frames between a client and the server.
//
// It is a Unix domain socket. The socket is a door into a process that runs
// commands on the user's behalf, so it is created private and kept private:
// anyone who can connect can start a process as this user.
package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrAlreadyRunning means a live server already holds the socket.
var ErrAlreadyRunning = errors.New("transport: a server is already listening")

// DefaultSessionName is the session used when none is given.
const DefaultSessionName = "default"

// dirPerm and socketPerm keep the socket reachable only by its owner. This is
// the access control: there is no authentication beyond the file system, so
// the permissions are the whole of it.
const (
	dirPerm    os.FileMode = 0o700
	socketPerm os.FileMode = 0o600
)

// checkPathLength rejects a socket path the kernel cannot bind.
//
// A Unix socket address is a fixed-size struct: the path lives in sun_path,
// which is 108 bytes on Linux and 104 on the BSDs including macOS. Exceeding
// it fails with a bare "invalid argument" that says nothing about why, so the
// check is here to turn that into an explanation and a way out.
func checkPathLength(path string) error {
	if len(path) < maxSocketPathLen {
		return nil
	}
	return fmt.Errorf(
		"transport: socket path is %d bytes, the limit is %d: %s\n"+
			"set TEND_RUNTIME_DIR to a shorter directory",
		len(path), maxSocketPathLen-1, path)
}

// SocketPath returns the socket for a named session.
//
// A session name is part of a file path, so it is restricted to characters
// that cannot walk out of the directory. Rejecting them is the point: a name
// arriving from a flag must not be able to place a socket somewhere else.
func SocketPath(name string) (string, error) {
	if name == "" {
		name = DefaultSessionName
	}
	if err := validSessionName(name); err != nil {
		return "", err
	}
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".sock")
	if err := checkPathLength(path); err != nil {
		return "", err
	}
	return path, nil
}

func validSessionName(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("transport: session name is too long")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("transport: session name %q contains %q; use letters, digits, dash, underscore or dot", name, r)
		}
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("transport: session name %q is not allowed", name)
	}
	return nil
}

// Listen creates the socket for a session and starts accepting on it.
//
// A socket left behind by a crashed server would otherwise block every later
// start, so a stale one is removed — but only after checking that nothing is
// listening on it. Removing a live server's socket would silently orphan it,
// leaving its panes running and unreachable.
func Listen(path string) (net.Listener, error) {
	if err := checkPathLength(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("transport: creating %s: %w", filepath.Dir(path), err)
	}

	if _, err := os.Stat(path); err == nil {
		if alive(path) {
			return nil, fmt.Errorf("%w at %s", ErrAlreadyRunning, path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("transport: removing stale socket: %w", err)
		}
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("transport: listening on %s: %w", path, err)
	}
	// Narrow the socket before anyone can reach it. A window where it is
	// world-writable is a window where anyone can run a command as this user.
	if err := os.Chmod(path, socketPerm); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("transport: securing %s: %w", path, err)
	}
	return ln, nil
}

// Dial connects to a server.
func Dial(path string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("transport: connecting to %s: %w", path, err)
	}
	return conn, nil
}

// alive reports whether something is accepting on the socket.
func alive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Sessions lists the sessions that currently have a socket, whether or not a
// server is still behind it.
func Sessions() ([]string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sock")
		if !ok {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
