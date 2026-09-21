package explorer

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// searchRepo is repo() with text to find: in a tracked file, a new one, an
// ignored one and a nested directory.
func searchRepo(t *testing.T) string {
	dir := repo(t)
	write(t, dir, "src/changed.go", "package src\n\nvar Needle = 1 // needle\nvar needles = 2\n")
	write(t, dir, "brandnew.md", "a needle in a new file\n")
	write(t, dir, "build/out.txt", "needle in an ignored file\n")
	write(t, dir, "docs/guide/deep.txt", "   indented needle here\n")
	return dir
}

func grepPaths(t *testing.T, dir string, inRepo bool, o GrepOptions) []string {
	t.Helper()
	matches, _, err := Grep(context.Background(), dir, inRepo, o)
	if err != nil {
		t.Fatalf("grep %+v: %v", o, err)
	}
	var out []string
	for _, m := range matches {
		out = append(out, m.Path+":"+strconv.Itoa(m.Line))
	}
	sort.Strings(out)
	return out
}

// TestGrepSearchesTheProjectAsAnEditorWould: tracked and new files are
// searched and ignored ones are not; the three switches and the globs do
// what an editor's do. If it regresses, the search misses what an agent
// just wrote, or buries the project in node_modules.
func TestGrepSearchesTheProjectAsAnEditorWould(t *testing.T) {
	dir := searchRepo(t)
	cases := []struct {
		name string
		o    GrepOptions
		want string
	}{
		{"case-insensitive by default", GrepOptions{Query: "needle"},
			"brandnew.md:1 docs/guide/deep.txt:1 src/changed.go:3 src/changed.go:4"},
		{"case", GrepOptions{Query: "Needle", Case: true}, "src/changed.go:3"},
		{"whole word", GrepOptions{Query: "needle", Word: true},
			"brandnew.md:1 docs/guide/deep.txt:1 src/changed.go:3"},
		{"regex", GrepOptions{Query: "needles? =", Regex: true}, "src/changed.go:3 src/changed.go:4"},
		{"text is text", GrepOptions{Query: "needles? ="}, ""},
		{"include", GrepOptions{Query: "needle", Include: "*.md, docs/**"}, "brandnew.md:1 docs/guide/deep.txt:1"},
		{"exclude", GrepOptions{Query: "needle", Exclude: "*.go"}, "brandnew.md:1 docs/guide/deep.txt:1"},
	}
	for _, c := range cases {
		if got := strings.Join(grepPaths(t, dir, true, c.o), " "); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if _, _, err := Grep(context.Background(), dir, true, GrepOptions{Query: "(", Regex: true}); err == nil {
		t.Error("a broken pattern should say so")
	}
}

// TestGrepOutsideARepository: a plain directory is searched too. If it
// regresses, the search view is empty everywhere git is not.
func TestGrepOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes/todo.txt", "buy milk\n")
	if got := strings.Join(grepPaths(t, dir, false, GrepOptions{Query: "milk"}), " "); got != "notes/todo.txt:1" {
		t.Errorf("got %q", got)
	}
}

// TestTheSearchViewFindsTextAndOpensTheEditorOnItsLine: typing searches
// (a moment after the last key), the results are grouped by file, enter
// opens the editor on the line, and space shows the file there. If it
// regresses, finding where something is written means leaving tend.
func TestTheSearchViewFindsTextAndOpensTheEditorOnItsLine(t *testing.T) {
	dir := searchRepo(t)
	opener := &recordingOpener{}
	m := New(dir, opener)
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	keys(m, "ctrl+f")
	for _, r := range "needles" {
		m.Key(Key{Name: string(r), Rune: r})
	}
	if _, due := m.SearchDue(clock); due {
		t.Fatal("a search started before the typing stopped")
	}
	clock = clock.Add(searchDelay)
	req, due := m.SearchDue(clock)
	if !due {
		t.Fatal("no search once the typing stopped")
	}
	m.ApplySearch(RunSearch(context.Background(), req))
	text := screen(m, 44, 16)
	for _, want := range []string{"› needles", "1 in 1 files", "changed.go src", "4 var needles = 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}

	// An answer to an older query is dropped.
	m.Key(Key{Name: "backspace"})
	m.ApplySearch(SearchResult{Gen: req.Gen, Matches: []GrepMatch{{Path: "stale", Line: 1}}})
	if len(m.csearch.results) == 1 && m.csearch.results[0].Path == "stale" {
		t.Error("a stale answer was shown")
	}
	m.searchNow()
	if len(m.csearch.results) != 4 {
		t.Fatalf("needle: %d results", len(m.csearch.results))
	}
	for _, r := range m.csearch.rows {
		if r.match < 0 && r.path == "src/changed.go" && r.count != 2 {
			t.Errorf("changed.go has two matches, its heading says %d", r.count)
		}
	}

	keys(m, "enter") // leave the query for the results
	keys(m, "j", "enter")
	if len(opener.opened) != 1 || opener.lines[0] == 0 {
		t.Fatalf("enter opens at the line: %v %v", opener.opened, opener.lines)
	}
	keys(m, "space")
	if text := screen(m, 44, 16); !strings.Contains(text, "‹ ") {
		t.Errorf("space shows the file:\n%s", text)
	}
	if m.viewer.mark != opener.lines[0] {
		t.Errorf("the viewer marks line %d, want %d", m.viewer.mark, opener.lines[0])
	}
}

// TestSearchSwitchesAndGlobsAreTyped: alt+c, alt+w and alt+r switch, tab
// goes to the include and exclude globs, and each change searches again.
func TestSearchSwitchesAndGlobsAreTyped(t *testing.T) {
	dir := searchRepo(t)
	m := New(dir, nil)
	keys(m, "2")
	for _, r := range "needle" {
		m.Key(Key{Name: string(r), Rune: r})
	}
	m.Key(Key{Name: "alt+w"})
	keys(m, "tab")
	for _, r := range "*.md" {
		m.Key(Key{Name: string(r), Rune: r})
	}
	m.searchNow()
	o := m.csearch.opts
	if !o.Word || o.Include != "*.md" || o.Query != "needle" {
		t.Fatalf("options = %+v", o)
	}
	if len(m.csearch.results) != 1 || m.csearch.results[0].Path != "brandnew.md" {
		t.Errorf("results = %+v", m.csearch.results)
	}
	if text := screen(m, 44, 16); !strings.Contains(text, "+*.md") || !strings.Contains(text, "include › *.md") {
		t.Errorf("the include glob is shown:\n%s", text)
	}
}

// TestParseReadsAltKeys: escape and a letter together is alt and the
// letter, which the search's switches are on. If it regresses, alt+c is an
// escape that leaves the query and a stray c typed after it.
func TestParseReadsAltKeys(t *testing.T) {
	events, _ := Parse([]byte("\x1bc\x1b"))
	if len(events) != 2 || events[0].(Key).Name != "alt+c" || events[1].(Key).Name != "esc" {
		t.Errorf("events = %+v", events)
	}
}

// TestEditorAtPutsTheCursorOnTheLine: editors that take +N get it; others
// are opened at the top rather than handed an argument they may read as a
// file. If it regresses, a search result opens the editor at line one, or
// opens a file called +12.
func TestEditorAtPutsTheCursorOnTheLine(t *testing.T) {
	cases := map[string]string{
		"nvim":            "nvim +12",
		"/usr/bin/vim -p": "/usr/bin/vim -p +12",
		"nano":            "nano +12",
		"code --wait":     "code --wait",
	}
	for editor, want := range cases {
		if got := editorAt(editor, 12); got != want {
			t.Errorf("editorAt(%q) = %q, want %q", editor, got, want)
		}
	}
	if got := editorAt("nvim", 0); got != "nvim" {
		t.Errorf("no line: %q", got)
	}
}
