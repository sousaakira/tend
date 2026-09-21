package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
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
