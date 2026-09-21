package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/client"
	"github.com/sousaakira/tend/internal/machines"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/transport"
)

// TestMachineCommandsKeepTheCatalog: herdr's `machine` — add with a label
// and a session, list, rename, disable, remove — through the binary. If it
// regresses, there is no way to tell a client about another machine.
func TestMachineCommandsKeepTheCatalog(t *testing.T) {
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_STATE_DIR="+t.TempDir(), "TEND_SSH=true")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"machine"}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("machine %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if out := run("add", "root@10.8.0.110", "-label", "server", "-session", "work"); !strings.Contains(out, "saved root@10.8.0.110 as server") {
		t.Fatalf("add: %s", out)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(run("list", "-json")), &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", err, list)
	}
	id := list[0]["id"].(string)
	if list[0]["session"] != "work" || list[0]["enabled"] != true {
		t.Errorf("saved %v", list[0])
	}
	run("rename", id, "-label", "box")
	run("disable", id)
	if out := run("list"); !strings.Contains(out, "box") || !strings.Contains(out, "off") {
		t.Errorf("after rename and disable:\n%s", out)
	}
	run("remove", id)
	if out := run("list"); !strings.Contains(out, "No saved machines") {
		t.Errorf("after remove:\n%s", out)
	}
}

// TestSavedMachinesAreListedAndGoneToFromTheSidebar: with a machine saved, the
// sidebar lists machines, each with its spaces — herdr's endpoint sidebar —
// and clicking the other machine's space shows it, and this machine's space
// brings the client back. If it regresses, a saved machine is a line in a
// file and nothing more.
//
// The far machine is a server in its own runtime directory, reached through a
// stand-in for ssh as TestAttachOverSSH reaches one.
func TestSavedMachinesAreListedAndGoneToFromTheSidebar(t *testing.T) {
	bin := buildBinary(t)
	far := t.TempDir()
	farPath := filepath.Join(far, "far.sock")
	ln, err := transport.Listen(farPath)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Config{Build: version, DetectInterval: 20 * time.Millisecond, ShutdownGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
		<-served
	})
	c, err := client.Dial(farPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := c.NewWorkspace("farspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.NewTab(ws, "t", proto.PaneSpec{Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	stand := filepath.Join(t.TempDir(), "fake-ssh")
	script := "#!/bin/sh\nshift; shift\nTEND_RUNTIME_DIR=" + far + " exec " + bin + " \"$@\"\n"
	if err := os.WriteFile(stand, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("TEND_STATE_DIR", state)
	var catalog machines.Catalog
	if _, err := catalog.Add("farbox", "user@farhost", "far"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Save(); err != nil {
		t.Fatal(err)
	}

	a := startSessionEnv(t, 110, 24, "TEND_STATE_DIR="+state, "TEND_SSH="+stand)
	a.waitForScreen(t, "both machines, and the far one's space", func(string) bool {
		s := a.sidebarText()
		return strings.Contains(s, "machines") && strings.Contains(s, "Local") &&
			strings.Contains(s, "farbox") && strings.Contains(s, "●") && strings.Contains(s, "farspace")
	})

	row := a.lineContaining(t, "farspace")
	a.clickAt(t, 8, row)
	a.waitForScreen(t, "the far machine shown", func(s string) bool {
		status := a.lines()[len(a.lines())-1]
		return strings.Contains(status, "user@farhost:far") && strings.Contains(a.sidebarText(), "new · farbox")
	})
	a.sendUntil(t, "printf FAR-OK\n", "the far shell to answer", func(s string) bool {
		return strings.Contains(s, "FAR-OK")
	})
	row = a.lineContaining(t, "main")
	a.clickAt(t, 8, row)
	a.waitForScreen(t, "this machine shown again", func(s string) bool {
		status := a.lines()[len(a.lines())-1]
		return !strings.Contains(status, "farhost") && strings.Contains(a.sidebarText(), "new · Local")
	})
}
