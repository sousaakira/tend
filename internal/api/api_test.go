package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/session"
)

type harness struct {
	srv  *server.Server
	conn net.Conn
	r    *bufio.Reader
	pane session.PaneID
	path string
}

func start(t *testing.T) *harness {
	t.Helper()
	srv, err := server.New(server.Config{
		DetectInterval: 10 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := srv.NewWorkspace("main")
	_, pane, err := srv.NewTab(ws, "t", server.PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "api.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	a := New(srv, "test-build", []string{"/bin/sh"})
	go func() { _ = a.Serve(ln) }()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		_ = ln.Close()
		a.Close()
		_ = srv.Close()
	})
	return &harness{srv: srv, conn: conn, r: bufio.NewReader(conn), pane: pane, path: path}
}

// another opens a second connection. Each connection is answered in order, so
// a caller that is waiting on one call needs another for anything that has to
// happen while it waits — which is also how two scripts use one session.
func (h *harness) another(t *testing.T) *harness {
	t.Helper()
	conn, err := net.Dial("unix", h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &harness{srv: h.srv, conn: conn, r: bufio.NewReader(conn), pane: h.pane, path: h.path}
}

// send writes one line without waiting for the reply, for the cases where
// something else is waiting for what it causes.
func (h *harness) send(line string) error {
	_, err := fmt.Fprintln(h.conn, line)
	return err
}

// next reads one more line from a connection that has become a stream.
func (h *harness) next(t *testing.T) map[string]any {
	t.Helper()
	_ = h.conn.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := h.r.ReadString('\n')
	if err != nil {
		t.Fatalf("the stream ended: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("stream line %q: %v", line, err)
	}
	return out
}

// call sends one line and reads one back, which is the whole protocol.
//
// The deadline is far longer than any wait these tests ask for. It used to be
// the same five seconds a wait was given, so a wait that ran its full course —
// which is what the test about timeouts asks for — answered at the instant the
// read gave up, and the test failed under load and passed on its own.
func (h *harness) call(t *testing.T, line string) map[string]any {
	t.Helper()
	_ = h.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintln(h.conn, line); err != nil {
		t.Fatal(err)
	}
	reply, err := h.r.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply to %s: %v", line, err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		t.Fatalf("reply %q is not JSON: %v", reply, err)
	}
	return out
}

func errorCode(reply map[string]any) string {
	body, _ := reply["error"].(map[string]any)
	code, _ := body["code"].(string)
	return code
}

// TestPaneEnvIsWhatAHookLooksFor: a hook decides whether to speak by reading
// these four names. If any is missing or mistyped, every integration exits
// before the first report and the pane falls back to screen detection alone.
func TestPaneEnvIsWhatAHookLooksFor(t *testing.T) {
	got := PaneEnv("/run/tend/api/work.sock", 7, "/usr/bin/tend")
	want := map[string]string{
		EnvMarker:     EnvMarkerValue,
		EnvSocketPath: "/run/tend/api/work.sock",
		EnvPaneID:     "p_7",
		EnvBinPath:    "/usr/bin/tend",
	}
	if len(got) != len(want) {
		t.Fatalf("PaneEnv = %v, want %d entries", got, len(want))
	}
	for _, kv := range got {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("entry %q is not KEY=value", kv)
		}
		if want[k] != v {
			t.Errorf("%s = %q, want %q", k, v, want[k])
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("missing %s", k)
	}

	withoutBin := PaneEnv("/tmp/a.sock", 1, "")
	for _, kv := range withoutBin {
		if strings.HasPrefix(kv, EnvBinPath+"=") {
			t.Errorf("an empty binary path must not be set: %v", withoutBin)
		}
	}
}

// TestAHookCanReportInOneLine is the contract every installed hook depends on:
// the exact request herdr's hooks send, with tend's names, must be understood.
// If it regresses, every integration goes quiet at once and nothing says why.
func TestAHookCanReportInOneLine(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	reply := h.call(t, `{"id":"a:1","method":"pane.report_agent","params":{"pane_id":"`+pane+
		`","source":"my-hook","agent":"deploy-bot","state":"blocked","message":"approve?","seq":1}}`)
	if reply["id"] != "a:1" || reply["error"] != nil {
		t.Fatalf("reply = %v", reply)
	}
	if result, _ := reply["result"].(map[string]any); result["type"] != "ok" {
		t.Errorf("result = %v, want type ok", reply["result"])
	}

	got := h.call(t, `{"id":"2","method":"pane.get","params":{"pane_id":"`+pane+`"}}`)
	info, _ := got["result"].(map[string]any)["pane"].(map[string]any)
	if info["agent"] != "deploy-bot" || info["agent_state"] != "blocked" || info["message"] != "approve?" {
		t.Errorf("pane = %v", info)
	}

	h.call(t, `{"id":"3","method":"pane.release_agent","params":{"pane_id":"`+pane+`","source":"my-hook","agent":"deploy-bot","seq":2}}`)
	got = h.call(t, `{"id":"4","method":"pane.get","params":{"pane_id":"`+pane+`"}}`)
	info, _ = got["result"].(map[string]any)["pane"].(map[string]any)
	if info["agent"] != nil {
		t.Errorf("after release, pane = %v", info)
	}
}

// TestMistakesGetACodeAndTheConnectionSurvives: a script has to be able to tell
// a missing pane from a typo in a method, and one bad line must not cost it the
// connection it was going to use for the next.
func TestMistakesGetACodeAndTheConnectionSurvives(t *testing.T) {
	h := start(t)
	for _, c := range []struct{ line, code string }{
		{`not json`, "invalid_request"},
		{`{"id":"1","method":"pane.explode"}`, "unknown_method"},
		{`{"id":"2","method":"pane.get","params":{"pane_id":"p_999"}}`, "pane_not_found"},
		{`{"id":"3","method":"pane.get","params":{"pane_id":"nonsense"}}`, "pane_not_found"},
		{`{"id":"4","method":"pane.report_agent","params":{"pane_id":"` + PaneID(h.pane) + `","source":"s","agent":" ","state":"idle"}}`, "invalid_agent"},
		{`{"id":"5","method":"pane.report_agent","params":{"pane_id":"` + PaneID(h.pane) + `","source":"s","agent":"a","state":"dancing"}}`, "invalid_params"},
	} {
		if got := errorCode(h.call(t, c.line)); got != c.code {
			t.Errorf("%s: code = %q, want %q", c.line, got, c.code)
		}
	}
	pong := h.call(t, `{"id":"6","method":"ping"}`)
	if result, _ := pong["result"].(map[string]any); result["type"] != "pong" || result["version"] != "test-build" {
		t.Errorf("ping = %v", pong)
	}
}

// TestIntegrationListAndInstallRoundTrip: scripts and the CLI both go through
// this socket. If list or install breaks here, every agent stays on screen
// detection with no way to opt into hooks.
func TestIntegrationListAndInstallRoundTrip(t *testing.T) {
	h := start(t)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	list := h.call(t, `{"id":"i1","method":"integration.list"}`)
	result, _ := list["result"].(map[string]any)
	if result["type"] != "integration_list" {
		t.Fatalf("list = %v", list)
	}
	integrations, _ := result["integrations"].([]any)
	if len(integrations) == 0 {
		t.Fatal("integration.list returned no targets")
	}

	install := h.call(t, `{"id":"i2","method":"integration.install","params":{"target":"claude"}}`)
	if install["error"] != nil {
		t.Fatalf("install = %v", install)
	}
	got := install["result"].(map[string]any)
	if got["type"] != "integration_install" || got["target"] != "claude" {
		t.Errorf("install result = %v", got)
	}
	hook := filepath.Join(dir, "hooks", "tend-agent-state.sh")
	if _, err := os.Stat(hook); err != nil {
		t.Fatalf("hook not written: %v", err)
	}

	un := h.call(t, `{"id":"i3","method":"integration.uninstall","params":{"target":"claude"}}`)
	if un["error"] != nil {
		t.Fatalf("uninstall = %v", un)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Errorf("hook still present after uninstall")
	}
}

// TestALineWithNoEndIsCutOff: the socket is reachable by anything running as
// the user, including a broken hook in a loop.
func TestALineWithNoEndIsCutOff(t *testing.T) {
	h := start(t)
	_ = h.conn.SetDeadline(time.Now().Add(10 * time.Second))
	chunk := strings.Repeat("x", 1<<16)
	for i := 0; i < 40; i++ {
		if _, err := h.conn.Write([]byte(chunk)); err != nil {
			return // hung up on, which is the point
		}
	}
	if _, err := h.r.ReadString('\n'); err == nil {
		t.Error("the server kept reading a request with no end")
	}
}
