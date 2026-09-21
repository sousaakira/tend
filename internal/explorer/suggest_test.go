package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude stands in for the claude CLI: it records what it was given and
// where, and answers with noise before a quoted line, as the real one can.
func fakeClaude(t *testing.T, reply string) (record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	script := "#!/bin/sh\n" +
		"echo \"args: $*\" > " + record + "\n" +
		"echo \"pane: ${TEND_PANE_ID:-none}\" >> " + record + "\n" +
		"cat >> " + record + "\n" +
		"echo 'Warning: an MCP server is slow'\n" +
		"printf '%s\\n' " + shellQuote(reply) + "\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := suggestCommand
	suggestCommand = path
	t.Cleanup(func() { suggestCommand = old })
	return record
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// TestADraftedMessageGoesIntoTheCommitBox: A asks claude about the staged
// diff, with haiku and the prompt herdr-sidebar uses, outside the pane's
// tend session, and puts its line, cleaned, in the commit box to be read
// before enter. If it regresses, the ✧ button types nothing, or the files
// panel shows up in the agent list as a claude.
func TestADraftedMessageGoesIntoTheCommitBox(t *testing.T) {
	withIdentity(t)
	t.Setenv("TEND_PANE_ID", "p_7")
	record := fakeClaude(t, `"Add the needle constant."`)
	dir := repo(t)
	gitIn(t, dir, "add", "src/changed.go")
	m := New(dir, nil)
	keys(m, "3", "A")
	m.runJobNow()
	if m.mode != modeCommit || m.input != "Add the needle constant" {
		t.Fatalf("mode %v, box %q", m.mode, m.input)
	}
	got, _ := os.ReadFile(record)
	for _, want := range []string{"-p --model haiku --strict-mcp-config", "pane: none", "+var x = 1"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("claude was given %q, missing %q", got, want)
		}
	}
	if strings.Contains(string(got), "brandnew.md") {
		t.Errorf("with something staged, only the staged diff is described:\n%s", got)
	}
	keys(m, "enter")
	if log := gitIn(t, dir, "log", "--oneline", "-1"); !strings.Contains(log, "Add the needle constant") {
		t.Errorf("enter commits the draft: %s", log)
	}
}

// TestWithoutClaudeTheMessageNamesTheFiles: no CLI, or one that fails, and
// the draft is herdr-sidebar's fallback. If it regresses, A on a machine
// without claude does nothing at all.
func TestWithoutClaudeTheMessageNamesTheFiles(t *testing.T) {
	old := suggestCommand
	suggestCommand = filepath.Join(t.TempDir(), "no-such-claude")
	t.Cleanup(func() { suggestCommand = old })
	dir := repo(t)
	m := New(dir, nil)
	keys(m, "3", "A")
	m.runJobNow()
	if m.input != "Update changed.go" {
		t.Errorf("fallback = %q", m.input)
	}
	if got := fallbackMessage([]string{"a/b.go", "c.go", "d.go"}); got != "Update b.go and 2 more" {
		t.Errorf("many files: %q", got)
	}
}

// TestCleanReplyTakesTheLastUsableLine is herdr-sidebar's cleanup: fences,
// quotes, a trailing period and warning noise go.
func TestCleanReplyTakesTheLastUsableLine(t *testing.T) {
	cases := map[string]string{
		"Fix the parser":                         "Fix the parser",
		"```\n\"Fix the parser.\"\n```":          "Fix the parser",
		"warning: slow\n`Add tests`\n":           "Add tests",
		"Error: boom\nReal line\nwarn: trailing": "Real line",
	}
	for in, want := range cases {
		if got, ok := cleanReply(in); !ok || got != want {
			t.Errorf("cleanReply(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := cleanReply("\n  \n"); ok {
		t.Error("an empty reply is no reply")
	}
}
