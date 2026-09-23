package explorer

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/vt"
)

func spanStyles(spans []span) map[string]vt.Style {
	out := map[string]vt.Style{}
	for _, s := range spans {
		out[strings.TrimSpace(s.text)] = s.style
	}
	return out
}

// TestHighlightColoursWhatMakesCodeReadable: keywords, strings, numbers and
// comments, a block comment carried across lines, and markdown's headings
// and fences. If it regresses, the preview is grey text again.
func TestHighlightColoursWhatMakesCodeReadable(t *testing.T) {
	g := langFor("main.go")
	spans, st := g.highlight(`func main() { x := "hi" + 42 // done`, hlState{})
	styles := spanStyles(spans)
	if styles["func"] != hlKeyword || styles[`"hi"`] != hlString || styles["42"] != hlNumber || styles["// done"] != hlComment {
		t.Errorf("go line: %+v", spans)
	}
	if styles["main"] != styleNormal {
		t.Errorf("a name is not a keyword: %+v", styles["main"])
	}
	_, st = g.highlight("a := 1 /* opens", st)
	if !st.inBlock {
		t.Fatal("a block comment left open should carry to the next line")
	}
	spans, st = g.highlight("still comment */ return", st)
	if st.inBlock || spanStyles(spans)["return"] != hlKeyword || spans[0].style != hlComment {
		t.Errorf("closing the block: %+v", spans)
	}

	md := langFor("README.md")
	if spans, _ := md.highlight("## Title", hlState{}); spans[0].style != hlHeading {
		t.Errorf("heading: %+v", spans)
	}
	_, st = md.highlight("```go", hlState{})
	if spans, _ := md.highlight("func x()", st); spans[0].style != hlString {
		t.Errorf("inside a fence: %+v", spans)
	}
	if langFor("notes.xyz") != nil {
		t.Error("an unknown kind of file is plain")
	}
	if spans, _ := (*lang)(nil).highlight("plain", hlState{}); len(spans) != 1 || spans[0].text != "plain" {
		t.Errorf("plain: %+v", spans)
	}
}

func previewText(p *Preview, cols, rows int) string {
	g := vt.NewGrid(cols, rows, 0)
	p.Draw(g)
	var b strings.Builder
	for y := 0; y < rows; y++ {
		row := g.Line(y)
		for x := 0; x < row.Len(); x++ {
			c := row.Cell(x)
			if c.Width == 0 {
				continue
			}
			if c.R == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(c.R)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestThePreviewShowsTheLineRetargetsAndFollowsEdits: the file with line
// numbers, the line asked for in view; a retarget typed into the pane shows
// another file; a change on disk is read again. If it regresses, the
// preview shows the top of a file found at line 400, or a stale copy of one
// an agent is editing.
func TestThePreviewShowsTheLineRetargetsAndFollowsEdits(t *testing.T) {
	dir := t.TempDir()
	var long strings.Builder
	for i := 1; i <= 100; i++ {
		long.WriteString("line number " + strconv.Itoa(i) + "\n")
	}
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.go")
	os.WriteFile(a, []byte(long.String()), 0o644)
	os.WriteFile(b, []byte("package b\n"), 0o644)

	p := NewPreview(a, 0, nil)
	previewText(p, 40, 12)
	p.load(a, 60)
	text := previewText(p, 40, 12)
	if !strings.Contains(text, "60 line number 60") || strings.Contains(text, "  1 line number 1\n") {
		t.Errorf("line 60 in view:\n%s", text)
	}

	events, _ := Parse([]byte(RetargetSequence(b, 1)))
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	r := events[0].(Retarget)
	p.load(r.Path, r.Line)
	if text := previewText(p, 40, 12); !strings.Contains(text, "b.go") || !strings.Contains(text, "1 package b") {
		t.Errorf("retargeted:\n%s", text)
	}

	time.Sleep(20 * time.Millisecond)
	os.WriteFile(b, []byte("package b\n\nvar edited = true\n"), 0o644)
	future := time.Now().Add(time.Second)
	os.Chtimes(b, future, future)
	p.Tick()
	if text := previewText(p, 40, 12); !strings.Contains(text, "var edited = true") {
		t.Errorf("an edit on disk is shown:\n%s", text)
	}
}

// TestThePanelKeepsOnePreviewTab: the first preview opens a tab named after
// the file in the panel's space, runs tend view there in the project and
// goes to it; the next is sent to that tab, renamed after the new file,
// rather than opening another. If it regresses, the preview squeezes the
// terminal being worked in, or every file looked at stacks a tab.
func TestThePanelKeepsOnePreviewTab(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type call struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	calls := make(chan call, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var c call
			_ = json.Unmarshal(line, &c)
			calls <- c
			var result any = map[string]any{}
			switch c.Method {
			case "pane.layout":
				result = map[string]any{"layout": map[string]any{"workspace_id": "w_1"}}
			case "tab.create":
				result = map[string]any{
					"tab":       map[string]any{"tab_id": "t_4"},
					"root_pane": map[string]any{"pane_id": "p_8"},
				}
			}
			reply, _ := json.Marshal(map[string]any{"id": "x", "result": result})
			_, _ = conn.Write(append(reply, '\n'))
			conn.Close()
		}
	}()
	drain := func() []call {
		var out []call
		for len(calls) > 0 {
			out = append(out, <-calls)
		}
		return out
	}

	o := &SessionOpener{Socket: sock, Pane: "p_1"}
	if err := o.Preview("/work/a.go", 12, "/work"); err != nil {
		t.Fatal(err)
	}
	got := drain()
	if len(got) != 3 || got[0].Method != "pane.layout" || got[1].Method != "tab.create" || got[2].Method != "pane.focus" {
		t.Fatalf("calls = %+v", got)
	}
	created := got[1].Params
	cmd, _ := created["command"].([]any)
	if created["workspace_id"] != "w_1" || created["name"] != "a.go" || created["close_on_exit"] != true || created["dir"] != "/work" ||
		len(cmd) != 6 || !strings.Contains(cmd[2].(string), "view -line") || cmd[4] != "/work/a.go" || cmd[5] != "12" {
		t.Errorf("tab.create = %+v", created)
	}
	if got[2].Params["pane_id"] != "p_8" {
		t.Errorf("the preview's tab is gone to: %+v", got[2].Params)
	}

	if err := o.Preview("/work/b.go", 0, "/work"); err != nil {
		t.Fatal(err)
	}
	got = drain()
	if len(got) != 3 || got[0].Method != "pane.send_text" || got[0].Params["pane_id"] != "p_8" ||
		got[0].Params["text"] != RetargetSequence("/work/b.go", 0) {
		t.Fatalf("the second preview reuses the tab: %+v", got)
	}
	if got[1].Method != "tab.rename" || got[1].Params["tab_id"] != "t_4" || got[1].Params["name"] != "b.go" {
		t.Errorf("the tab is renamed after the file: %+v", got[1])
	}
	if got[2].Method != "pane.focus" || got[2].Params["pane_id"] != "p_8" {
		t.Errorf("and gone to: %+v", got[2])
	}
}
