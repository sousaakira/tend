package agentsessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	idA = "11111111-2222-3333-4444-555555555555"
	idB = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	idC = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
)

// writeSession puts a conversation where Claude Code keeps it.
func writeSession(t *testing.T, root, folder, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "projects", folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestConversationsAreListedByTheirNameAndPlace: each conversation comes
// with the name the user gave it over the one Claude Code made up, that
// over the first thing typed, and the directory it was held in; what a
// tool or a command put in as the user's is not what was typed; the last
// written comes first. If it regresses, the sessions list shows ids, or a
// command's output, where the user looks for what the conversation was.
func TestConversationsAreListedByTheirNameAndPlace(t *testing.T) {
	root := t.TempDir()
	a := writeSession(t, root, "-work-shop", idA,
		`{"type":"user","cwd":"/work/shop","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","cwd":"/work/shop","origin":{"kind":"human"},"message":{"role":"user","content":"fix the   checkout\nbutton"}}`,
		`{"type":"user","cwd":"/work/shop","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"ai-title","aiTitle":"Checkout button","sessionId":"`+idA+`"}`,
		`{"type":"user","cwd":"/work/shop","message":{"role":"user","content":[{"type":"text","text":"and the footer"}]}}`,
		`not json at all`,
	)
	b := writeSession(t, root, "-work-blog", idB,
		`{"type":"user","cwd":"/work/blog","message":{"role":"user","content":"write a post"}}`,
		`{"type":"ai-title","aiTitle":"Blog post"}`,
		`{"type":"agent-name","agentName":"The launch post"}`,
	)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}

	list, err := NewClaude(root).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
	if got := list[0]; got.ID != idB || got.Title != "The launch post" || got.Dir != "/work/blog" || got.Prompts != 1 {
		t.Errorf("newest: %+v", got)
	}
	if got := list[1]; got.ID != idA || got.Title != "Checkout button" || got.Dir != "/work/shop" || got.Prompts != 2 || got.Agent != "claude" {
		t.Errorf("older: %+v", got)
	}
	_ = b

	// With no title, the first thing typed, on one line.
	writeSession(t, root, "-work-shop", idC,
		`{"type":"user","cwd":"/work/shop","message":{"role":"user","content":"fix the   checkout\nbutton"}}`)
	list, _ = NewClaude(root).List()
	for _, s := range list {
		if s.ID == idC && s.Title != "fix the checkout button" {
			t.Errorf("untitled: %q", s.Title)
		}
	}
}

// TestAConversationThatNeverSaidWhereTakesItsFolders: a file Claude Code
// only named, with no line saying the directory, is listed where the
// others in its folder were, not at a path guessed from the folder's name,
// which turns every - into a /. If it regresses, such a session is shown,
// and resumed, in a directory that does not exist.
func TestAConversationThatNeverSaidWhereTakesItsFolders(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "-work-my-shop", idA,
		`{"type":"user","cwd":"/work/my-shop","message":{"role":"user","content":"hi"}}`)
	writeSession(t, root, "-work-my-shop", idB, `{"type":"agent-name","agentName":"stub"}`)
	list, err := NewClaude(root).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Dir != "/work/my-shop" {
			t.Errorf("%s in %q", s.Title, s.Dir)
		}
	}
}

// TestDeletingAConversationTakesItsFilesAndNothingElse: a delete removes the
// conversation and its subagents' folder, and leaves the project's other
// conversations and Claude Code's memory beside them; a name that is not a
// conversation's id deletes nothing. If it regresses, deleting old sessions
// takes the wrong files, or a crafted name reaches outside the folder.
func TestDeletingAConversationTakesItsFilesAndNothingElse(t *testing.T) {
	root := t.TempDir()
	a := writeSession(t, root, "-work", idA, `{"type":"user","cwd":"/work","message":{"role":"user","content":"a"}}`)
	b := writeSession(t, root, "-work", idB, `{"type":"user","cwd":"/work","message":{"role":"user","content":"b"}}`)
	sub := filepath.Join(filepath.Dir(a), idA, "subagents")
	memory := filepath.Join(filepath.Dir(a), "memory", "MEMORY.md")
	for _, p := range []string{filepath.Join(sub, "agent.jsonl"), memory} {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	c := NewClaude(root)
	if list, _ := c.List(); len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
	if err := c.Delete(idA); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{a, filepath.Join(filepath.Dir(a), idA)} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{b, memory} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s went too: %v", kept, err)
		}
	}
	if list, _ := c.List(); len(list) != 1 || list[0].ID != idB {
		t.Errorf("after: %+v", list)
	}
	for _, bad := range []string{idA, "../../etc", "memory", "*"} {
		if err := c.Delete(bad); !errors.Is(err, ErrNoSuchSession) {
			t.Errorf("Delete(%q) = %v", bad, err)
		}
	}
}

// TestNoClaudeIsNoConversations: a machine where Claude Code never ran lists
// nothing, without an error for the sessions list to show.
func TestNoClaudeIsNoConversations(t *testing.T) {
	list, err := NewClaude(filepath.Join(t.TempDir(), "missing")).List()
	if err != nil || len(list) != 0 {
		t.Errorf("list = %v, %v", list, err)
	}
}
