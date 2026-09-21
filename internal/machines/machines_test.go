package machines

import (
	"os"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/transport"
)

// TestTheCatalogKeepsMachinesAsHerdrDoes: saved, found, relabelled,
// turned off, removed, and written whole and private; what herdr refuses
// is refused. If it regresses, a saved machine is lost, or a password in a
// target is written to disk.
func TestTheCatalogKeepsMachinesAsHerdrDoes(t *testing.T) {
	t.Setenv(transport.StateDirEnv, t.TempDir())
	c, err := Load()
	if err != nil || len(c.Machines) != 0 {
		t.Fatalf("empty: %v %+v", err, c)
	}
	m, err := c.Add("build box", "root@10.8.0.110", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Session != "default" || !m.Enabled || !strings.HasPrefix(m.ID, "m_") {
		t.Errorf("added %+v", m)
	}
	if _, err := c.Add("again", "root@10.8.0.110", "default"); err == nil {
		t.Error("the same target and session twice")
	}
	for what, target := range map[string]string{
		"a password":     "root:secret@host",
		"a flag":         "-oProxyCommand=x",
		"a space":        "root@ho st",
		"a control char": "root@host\n",
	} {
		if _, err := c.Add("x", target, ""); err == nil {
			t.Errorf("%s in a target was taken", what)
		}
	}
	if _, err := c.Add("", "other", ""); err == nil {
		t.Error("no label was taken")
	}
	if err := c.Rename(m.ID, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file: %v %v", err, info.Mode())
	}
	again, err := Load()
	if err != nil || len(again.Machines) != 1 || again.Machines[0].Label != "ci" {
		t.Fatalf("reloaded: %v %+v", err, again)
	}
	found, _ := again.Find(m.ID)
	found.Enabled = false
	if len(again.Enabled()) != 0 {
		t.Error("an off machine is not enabled")
	}
	if err := again.Remove(m.ID); err != nil || len(again.Machines) != 0 {
		t.Errorf("remove: %v", err)
	}
	if err := again.Remove("m_nope"); err == nil {
		t.Error("removing what is not there")
	}
}
