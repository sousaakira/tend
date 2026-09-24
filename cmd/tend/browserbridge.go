package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/browserext"
)

// The browser tend opens and the bridge its extension talks through
// (internal/browserext, docs/BROWSER.md).
//
// launch writes the extension and the bridge's registration into a profile
// of the session's own and starts a Chromium-family browser on it, with the
// extension loaded: nothing to install by hand, and the user's own browser
// untouched. The browser starts the bridge when the extension connects,
// with the environment it was started with, which names the session.
//
// bridge is the native messaging host: the browser writes the extension's
// messages to its stdin and reads its replies from its stdout, each a JSON
// object after four bytes of its length. It attaches to the session as a
// browser and passes each command on; what the extension sends — only the
// browser's own methods — goes to the session, and the answer comes back.

// Environment the browser is started with, for the bridge it starts.
const (
	browserSessionEnv = "TEND_BROWSER_SESSION"
	browserRemoteEnv  = "TEND_BROWSER_REMOTE"
)

// launchTendBrowser opens a page in the session's browser, starting it if
// it is not running (a second start on the same profile is a new tab in the
// one that is), and says which browser it used.
func launchTendBrowser(session, remote, program, url string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	root, err := browserext.DefaultRoot()
	if err != nil {
		return "", err
	}
	if session == "" {
		session = "default"
	}
	profile, err := browserext.Prepare(filepath.Join(root, session), self)
	if err != nil {
		return "", err
	}
	bin, err := browserext.Find(program, nil)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, browserext.Args(profile, url)...)
	cmd.Env = append(os.Environ(), browserSessionEnv+"="+session, browserRemoteEnv+"="+remote)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { _ = cmd.Wait() }()
	return filepath.Base(bin), nil
}

// bridgeMethods are what the extension may ask of the session: the
// browser's own, nothing that drives panes or the server.
var bridgeMethods = map[string]bool{
	api.MethodBrowserContext:     true,
	api.MethodBrowserSendToAgent: true,
	api.MethodBrowserStatus:      true,
}

// nativeWriter writes native messages, one at a time.
type nativeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (n *nativeWriter) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var size [4]byte
	binary.NativeEndian.PutUint32(size[:], uint32(len(raw)))
	if _, err := n.w.Write(size[:]); err != nil {
		return err
	}
	_, err = n.w.Write(raw)
	return err
}

// readNative reads one native message.
func readNative(r io.Reader) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	n := binary.NativeEndian.Uint32(size[:])
	if n > 64<<20 {
		return nil, fmt.Errorf("a message of %d bytes", n)
	}
	raw := make([]byte, n)
	_, err := io.ReadFull(r, raw)
	return raw, err
}

// runBrowserBridge is `tend browser bridge`, the native messaging host.
func runBrowserBridge() error {
	return bridge(os.Stdin, os.Stdout, os.Getenv(browserSessionEnv), os.Getenv(browserRemoteEnv))
}

func bridge(in io.Reader, out io.Writer, session, remote string) error {
	if session == "" {
		session = "default"
	}
	if remote != "" {
		remoteHost = remote
	}
	w := &nativeWriter{w: out}
	conn, err := dialAPI(session)
	if err != nil {
		_ = w.write(map[string]any{"type": "error", "message": "no tend session " + session + ": " + err.Error()})
		return err
	}
	defer conn.Close()
	req, _ := json.Marshal(map[string]any{"id": "bridge", "method": api.MethodBrowserAttach,
		"params": map[string]any{"name": "tend browser"}})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<16)
	first, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.Contains(first, "browser_attached") {
		_ = w.write(map[string]any{"type": "error", "message": strings.TrimSpace(first)})
		return errors.New("attach: " + strings.TrimSpace(first))
	}

	// The session's commands, out to the extension as they come. When the
	// session goes, so does the bridge; the extension starts another.
	done := make(chan error, 2)
	go func() {
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				done <- nil
				return
			}
			var cmd json.RawMessage
			if json.Unmarshal(line, &cmd) != nil {
				continue
			}
			if err := w.write(cmd); err != nil {
				done <- err
				return
			}
		}
	}()
	// The extension's requests, to the session and back.
	go func() {
		for {
			raw, err := readNative(in)
			if err != nil {
				done <- nil // the browser closed the port
				return
			}
			var msg struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			reply := map[string]any{"type": "reply", "id": msg.ID}
			if !bridgeMethods[msg.Method] {
				reply["error"] = map[string]any{"message": msg.Method + " is not the browser's to ask"}
			} else if result, err := apiCall(session, msg.Method, msg.Params, false); err != nil {
				reply["error"] = map[string]any{"message": err.Error()}
			} else {
				reply["result"] = result
			}
			if err := w.write(reply); err != nil {
				done <- err
				return
			}
		}
	}()
	return <-done
}
