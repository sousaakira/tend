//go:build unix

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// startRealSession is a session run as a user runs one, with its automation
// socket, for the tests that go through it from outside.
func startRealSession(t *testing.T, name string) (bin string, env []string) {
	t.Helper()
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env = append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	bin = buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", name}, pty.Options{Size: pty.Size{Cols: 100, Rows: 30}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 30, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, name)
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	return bin, env
}

func tendOutput(t *testing.T, bin string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func waitAttached(t *testing.T, bin string, env []string, session string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(tendOutput(t, bin, env, "browser", "status", "-s", session), "tend browser") {
		if time.Now().After(deadline) {
			t.Fatal("no browser attached")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func nativeFrame(v any) []byte {
	raw, _ := json.Marshal(v)
	var b bytes.Buffer
	_ = binary.Write(&b, binary.NativeEndian, uint32(len(raw)))
	b.Write(raw)
	return b.Bytes()
}

// TestTheBridgeCarriesTheExtensionsWay: the native messaging host, spoken
// to as a browser speaks to it, attaches to the session, passes on the
// commands the session gives, takes a capture to the context and answers
// it, and refuses what is not the browser's to ask. If it regresses, the
// extension loads and nothing it does reaches tend.
func TestTheBridgeCarriesTheExtensionsWay(t *testing.T) {
	bin, env := startRealSession(t, "bridge")
	host := exec.Command(bin, "browser", "bridge", "chrome-extension://"+"kafdikfjfbpngnlobakdlnepmciniffa/")
	host.Env = append(env, browserSessionEnv+"=bridge")
	in, _ := host.StdinPipe()
	out, _ := host.StdoutPipe()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = host.Process.Kill(); _ = host.Wait() })
	messages := make(chan map[string]any, 8)
	go func() {
		for {
			raw, err := readNative(out)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			messages <- m
		}
	}()
	next := func(what string) map[string]any {
		t.Helper()
		select {
		case m := <-messages:
			return m
		case <-time.After(10 * time.Second):
			t.Fatalf("no %s", what)
			return nil
		}
	}
	waitAttached(t, bin, env, "bridge")

	tendOutput(t, bin, env, "browser", "open", "-s", "bridge", "https://example.com/login")
	if m := next("command"); m["type"] != "browser_command" || m["action"] != "open" || m["url"] != "https://example.com/login" {
		t.Errorf("command: %v", m)
	}

	_, _ = in.Write(nativeFrame(map[string]any{"id": "7", "method": "browser.context",
		"params": map[string]any{"url": "https://example.com/login", "selector": "#email", "tag": "input"}}))
	if m := next("reply"); m["type"] != "reply" || m["id"] != "7" || m["error"] != nil {
		t.Errorf("reply: %v", m)
	}
	if got := tendOutput(t, bin, env, "context", "list", "-s", "bridge"); !strings.Contains(got, "#email") {
		t.Errorf("the capture in the context: %q", got)
	}

	_, _ = in.Write(nativeFrame(map[string]any{"id": "8", "method": "server.stop"}))
	if m := next("refusal"); m["id"] != "8" || m["error"] == nil {
		t.Errorf("server.stop is not the browser's: %v", m)
	}
}

// TestChromiumComesUpWithTheExtensionWorking: the browser tend opens, a real
// Chromium (headless here), has the extension loaded, which starts the
// bridge and attaches, and follows the session's open to a page. Skipped
// where there is no Chromium. If it regresses, the browser opens without
// the extension, or with it and no way to tend.
func TestChromiumComesUpWithTheExtensionWorking(t *testing.T) {
	chromium, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("needs chromium")
	}
	bin, env := startRealSession(t, "chromium")
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<title>tend test</title><h1 id="it">picked</h1>`))
	}))
	defer page.Close()

	data := t.TempDir()
	cmd := exec.Command(bin, "browser", "launch", "-s", "chromium")
	cmd.Env = append(env, "XDG_DATA_HOME="+data, "PATH=/nonexistent") // prepare only: no browser to start
	_ = cmd.Run()
	profile := filepath.Join(data, "tend", "browser", "chromium")
	if _, err := os.Stat(filepath.Join(profile, "extension", "manifest.json")); err != nil {
		t.Fatalf("launch prepared no profile: %v", err)
	}
	port := freePort(t)
	browser := exec.Command(chromium, "--headless=new", "--user-data-dir="+filepath.Join(profile, "profile"),
		"--load-extension="+filepath.Join(profile, "extension"), "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port="+port, "about:blank")
	browser.Env = append(env, browserSessionEnv+"=chromium")
	if err := browser.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browser.Process.Kill(); _ = browser.Wait() })
	waitAttached(t, bin, env, "chromium")

	tendOutput(t, bin, env, "browser", "open", "-s", "chromium", page.URL+"/it")
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/json/list")
		if err == nil {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(raw), page.URL+"/it") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the browser never opened the page it was sent")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}
