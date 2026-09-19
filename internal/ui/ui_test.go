package ui

import (
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/vt"
)

// gridText renders a grid as lines, so a whole screenful can be asserted as
// text instead of cell by cell.
func gridText(g *vt.Grid) []string {
	out := make([]string, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out[y] = g.Line(y).Text()
	}
	return out
}

func screenWith(t *testing.T, cols, rows int, input string) *vt.Screen {
	t.Helper()
	s := vt.NewScreen(cols, rows, 0)
	if _, err := s.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	return s
}

// --- drawing ---------------------------------------------------------------

func TestDrawSinglePane(t *testing.T) {
	dst := vt.NewGrid(20, 6, 0)
	Draw(dst, Frame{
		Session: "demo",
		Panes: []Pane{{
			ID:      1,
			Rect:    Rect{X: 0, Y: 0, Cols: 20, Rows: 5},
			Title:   "sh",
			Screen:  screenWith(t, 18, 3, "hello"),
			Running: true,
			Focused: true,
		}},
	}, DefaultTheme())

	lines := gridText(dst)
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "┐") {
		t.Errorf("top border = %q", lines[0])
	}
	if !strings.Contains(lines[0], "sh") {
		t.Errorf("the title should be in the top border: %q", lines[0])
	}
	if !strings.Contains(lines[1], "hello") {
		t.Errorf("the pane contents should be inside: %q", lines[1])
	}
	if !strings.HasPrefix(lines[4], "└") {
		t.Errorf("bottom border = %q", lines[4])
	}
}

// TestDrawKeepsPanesInTheirRects is what stops one pane painting over another:
// everything a pane draws, border included, stays inside the rectangle it was
// given.
func TestDrawKeepsPanesInTheirRects(t *testing.T) {
	dst := vt.NewGrid(40, 11, 0)
	left := screenWith(t, 18, 8, strings.Repeat("L", 200))
	right := screenWith(t, 18, 8, strings.Repeat("R", 200))

	Draw(dst, Frame{
		Panes: []Pane{
			{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 20, Rows: 10}, Screen: left, Running: true},
			{ID: 2, Rect: Rect{X: 20, Y: 0, Cols: 20, Rows: 10}, Screen: right, Running: true},
		},
	}, DefaultTheme())

	for y := 1; y < 9; y++ {
		row := dst.Line(y)
		for x := 0; x < 20; x++ {
			if r := row.Cell(x).R; r == 'R' {
				t.Fatalf("the right pane wrote into the left one at (%d,%d)", x, y)
			}
		}
		for x := 20; x < 40; x++ {
			if r := row.Cell(x).R; r == 'L' {
				t.Fatalf("the left pane wrote into the right one at (%d,%d)", x, y)
			}
		}
	}
}

func TestDrawPreservesPaneStyles(t *testing.T) {
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{
		Panes: []Pane{{
			ID:      1,
			Rect:    Rect{X: 0, Y: 0, Cols: 20, Rows: 4},
			Screen:  screenWith(t, 18, 2, "\x1b[31mred"),
			Running: true,
		}},
	}, DefaultTheme())

	// The pane's content starts one cell in from its border.
	if got := dst.Line(1).Cell(1).Style.FG; got != vt.IndexedColor(1) {
		t.Errorf("colour = %#v, want indexed 1; styling is lost in the blit", got)
	}
}

// TestPaneLabelFallsBackToTheCommand: a pane that has not named itself should
// say what it is running. "pane" tells nobody anything.
func TestPaneLabelFallsBackToTheCommand(t *testing.T) {
	dst := vt.NewGrid(30, 4, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 30, Rows: 3}, Command: "zsh", Running: true},
	}}, DefaultTheme())
	if got := dst.Line(0).Text(); !strings.Contains(got, "zsh") {
		t.Errorf("title = %q, want the command name", got)
	}

	// A title the pane set for itself wins over both.
	dst = vt.NewGrid(30, 4, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 30, Rows: 3}, Title: "my-project", Agent: "claude", Command: "zsh", Running: true},
	}}, DefaultTheme())
	if got := dst.Line(0).Text(); !strings.Contains(got, "my-project") {
		t.Errorf("title = %q, want the pane's own title", got)
	}
}

func TestDrawFocusedPaneIsDistinct(t *testing.T) {
	theme := DefaultTheme()

	focused := vt.NewGrid(20, 5, 0)
	Draw(focused, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true, Focused: true},
	}}, theme)

	plain := vt.NewGrid(20, 5, 0)
	Draw(plain, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true},
	}}, theme)

	if focused.Line(0).Cell(0).Style == plain.Line(0).Cell(0).Style {
		t.Error("a focused pane should look different from an unfocused one")
	}
}

// TestDrawSkipsPanesTooSmallForABorder: half a frame reads as a glitch, an
// empty space reads as a small pane.
func TestDrawSkipsPanesTooSmallForABorder(t *testing.T) {
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 1, Rows: 4}, Running: true},
	}}, DefaultTheme())

	if got := dst.Line(0).Text(); got != "" {
		t.Errorf("row 0 = %q, want nothing drawn", got)
	}
}

func TestDrawHandlesANilScreen(t *testing.T) {
	// A pane between being opened and its first output has no screen yet.
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true},
	}}, DefaultTheme())

	if !strings.HasPrefix(dst.Line(0).Text(), "┌") {
		t.Error("a pane with no screen should still be framed")
	}
}

// TestDrawClipsWideCharactersAtTheEdge: a double-width character whose second
// half falls outside the pane has nowhere to put it, so it must be replaced
// rather than drawn into the border.
func TestDrawClipsWideCharactersAtTheEdge(t *testing.T) {
	dst := vt.NewGrid(8, 4, 0)
	Draw(dst, Frame{Panes: []Pane{{
		ID:      1,
		Rect:    Rect{X: 0, Y: 0, Cols: 8, Rows: 3},
		Screen:  screenWith(t, 6, 1, "世界世"),
		Running: true,
	}}}, DefaultTheme())

	// The border column must survive.
	if got := dst.Line(1).Cell(7).R; got != '│' {
		t.Errorf("the right border was overwritten: %q", got)
	}
}

func TestDrawStatusBar(t *testing.T) {
	dst := vt.NewGrid(60, 6, 0)
	Draw(dst, Frame{
		Session:   "demo",
		Workspace: "main",
		Tab:       "work",
		Panes: []Pane{
			{ID: 1, Rect: Rect{Cols: 30, Rows: 5}, Agent: "claude", State: "working", Running: true, Focused: true},
			{ID: 2, Rect: Rect{X: 30, Cols: 30, Rows: 5}, Agent: "codex", State: "idle", Running: true},
		},
	}, DefaultTheme())

	status := dst.Line(dst.Rows() - 1).Text()
	for _, want := range []string{"demo", "main", "work", "1:claude", "2:codex"} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q is missing %q", status, want)
		}
	}
}

func TestDrawStatusShowsPrefix(t *testing.T) {
	dst := vt.NewGrid(40, 4, 0)
	Draw(dst, Frame{Session: "s", Prefix: true}, DefaultTheme())
	if got := dst.Line(3).Text(); !strings.Contains(got, "PREFIX") {
		t.Errorf("status = %q, want it to show the armed prefix", got)
	}
}

func TestDrawStatusShowsMessage(t *testing.T) {
	dst := vt.NewGrid(60, 4, 0)
	Draw(dst, Frame{Session: "s", Message: "pane 2 closed"}, DefaultTheme())
	if got := dst.Line(3).Text(); !strings.Contains(got, "pane 2 closed") {
		t.Errorf("status = %q", got)
	}
}

// TestDrawClearsStaleContent: a frame describes the whole screen, so anything
// from the last one is stale by definition.
func TestDrawClearsStaleContent(t *testing.T) {
	dst := vt.NewGrid(30, 6, 0)
	theme := DefaultTheme()

	Draw(dst, Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 30, Rows: 5},
		Screen: screenWith(t, 28, 3, "first frame"), Running: true,
	}}}, theme)

	Draw(dst, Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 30, Rows: 5},
		Screen: screenWith(t, 28, 3, "second"), Running: true,
	}}}, theme)

	for _, line := range gridText(dst) {
		if strings.Contains(line, "first frame") {
			t.Fatalf("stale content survived a redraw: %q", line)
		}
	}
}

func TestInnerSize(t *testing.T) {
	cols, rows := InnerSize(Rect{Cols: 20, Rows: 10})
	if cols != 18 || rows != 8 {
		t.Errorf("InnerSize = %dx%d, want 18x8", cols, rows)
	}
	// A rect too small for a border must still yield a usable size rather
	// than zero, which most programs refuse to draw into.
	cols, rows = InnerSize(Rect{Cols: 1, Rows: 1})
	if cols < 1 || rows < 1 {
		t.Errorf("InnerSize = %dx%d, want at least 1x1", cols, rows)
	}
}

func TestCursorFollowsTheFocusedPane(t *testing.T) {
	screen := screenWith(t, 18, 8, "abc")
	f := Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 20, Rows: 10}, Screen: screen, Running: true},
		{ID: 2, Rect: Rect{X: 20, Y: 0, Cols: 20, Rows: 10}, Screen: screen, Running: true, Focused: true},
	}}

	x, y, visible := CursorPosition(f)
	if !visible {
		t.Fatal("the cursor should be visible")
	}
	// The focused pane starts at x=20, its border takes one column, and the
	// pane's own cursor sits after "abc".
	if x != 24 || y != 1 {
		t.Errorf("cursor = (%d,%d), want (24,1)", x, y)
	}
}

func TestCursorHiddenForAnExitedPane(t *testing.T) {
	f := Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 20, Rows: 10},
		Screen: screenWith(t, 18, 8, "x"), Focused: true, Running: false,
	}}}
	if _, _, visible := CursorPosition(f); visible {
		t.Error("an exited pane should not show a cursor")
	}
}

func TestCursorHiddenWithNoFocus(t *testing.T) {
	if _, _, visible := CursorPosition(Frame{}); visible {
		t.Error("an empty frame has no cursor")
	}
}

// --- keys ------------------------------------------------------------------

func TestInputForwardsOrdinaryKeys(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte("hello world"))
	if string(forward) != "hello world" {
		t.Errorf("forwarded %q", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

// TestInputForwardsEscapeSequencesUntouched: an agent's own key handling needs
// the bytes exactly as typed, including sequences the client cannot name.
func TestInputForwardsEscapeSequencesUntouched(t *testing.T) {
	var in Input
	seq := "\x1b[1;5A\x1b[200~pasted\x1b[201~\x1bOP"
	forward, commands, _ := in.FeedAll([]byte(seq))
	if string(forward) != seq {
		t.Errorf("forwarded %q, want %q", forward, seq)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

func TestInputPrefixCommands(t *testing.T) {
	cases := map[string]Command{
		"|": CommandSplitColumns,
		"%": CommandSplitColumns,
		"-": CommandSplitRows,
		`"`: CommandSplitRows,
		"h": CommandFocusLeft,
		"l": CommandFocusRight,
		"k": CommandFocusUp,
		"j": CommandFocusDown,
		"o": CommandFocusNext,
		"x": CommandClosePane,
		"c": CommandNewTab,
		"n": CommandNextTab,
		"p": CommandPrevTab,
		"d": CommandDetach,
		"r": CommandRefresh,
		"?": CommandHelp,
	}
	for key, want := range cases {
		var in Input
		forward, commands, _ := in.FeedAll(append([]byte{Prefix}, key...))
		if len(commands) != 1 || commands[0] != want {
			t.Errorf("prefix %q gave %v, want %v", key, commands, want)
		}
		if len(forward) != 0 {
			t.Errorf("prefix %q forwarded %q, want nothing", key, forward)
		}
	}
}

func TestInputPrefixArrowKeys(t *testing.T) {
	cases := map[string]Command{
		"\x1b[A": CommandFocusUp,
		"\x1b[B": CommandFocusDown,
		"\x1b[C": CommandFocusRight,
		"\x1b[D": CommandFocusLeft,
	}
	for seq, want := range cases {
		var in Input
		_, commands, _ := in.FeedAll(append([]byte{Prefix}, seq...))
		if len(commands) != 1 || commands[0] != want {
			t.Errorf("prefix %q gave %v, want %v", seq, commands, want)
		}
	}
}

// TestInputArrowKeysArrivingApart: the three bytes of an arrow key can be read
// in separate chunks, and the state machine has to survive that.
func TestInputArrowKeysArrivingApart(t *testing.T) {
	var in Input
	var commands []Command
	for _, chunk := range [][]byte{{Prefix}, {0x1b}, {'['}, {'C'}} {
		_, cmds, _ := in.FeedAll(chunk)
		commands = append(commands, cmds...)
	}
	if len(commands) != 1 || commands[0] != CommandFocusRight {
		t.Errorf("commands = %v, want focus-right", commands)
	}
}

// TestInputDoublePrefixSendsItLiterally is how an inner multiplexer, or an
// editor bound to Ctrl+B, still receives the key.
func TestInputDoublePrefixSendsItLiterally(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, Prefix})
	if len(forward) != 1 || forward[0] != Prefix {
		t.Errorf("forwarded %q, want the prefix byte", forward)
	}
	if len(commands) != 1 || commands[0] != CommandLiteralPrefix {
		t.Errorf("commands = %v", commands)
	}
}

// TestInputUnboundKeyIsForwarded: a mistyped command must not silently eat the
// keystroke after it.
func TestInputUnboundKeyIsForwarded(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, 'Z'})
	if string(forward) != "Z" {
		t.Errorf("forwarded %q, want Z", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

// TestInputPrefixThenNonArrowEscape: ESC after the prefix that turns out not
// to be an arrow key releases what it collected rather than guessing.
func TestInputPrefixThenNonArrowEscape(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, 0x1b, 'O'})
	if string(forward) != "\x1bO" {
		t.Errorf("forwarded %q, want the collected bytes", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

func TestInputArmedReporting(t *testing.T) {
	var in Input
	if in.Armed() {
		t.Error("a fresh Input is not armed")
	}
	in.Feed(Prefix)
	if !in.Armed() {
		t.Error("the prefix should arm it")
	}
	in.Feed('x')
	if in.Armed() {
		t.Error("a command should disarm it")
	}
}

// TestInputKeepsOrderWithinAChunk: a chunk can hold both a command and text,
// and they must stay in order relative to each other.
func TestInputKeepsOrderWithinAChunk(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{'a', Prefix, 'x', 'b'})
	if string(forward) != "ab" {
		t.Errorf("forwarded %q, want ab", forward)
	}
	if len(commands) != 1 || commands[0] != CommandClosePane {
		t.Errorf("commands = %v", commands)
	}
}

// TestHelpMatchesTheBindings keeps the help text from drifting away from what
// the keys actually do.
func TestHelpMatchesTheBindings(t *testing.T) {
	lines := HelpLines()
	if len(lines) != len(Keys)+1 {
		t.Fatalf("help has %d lines for %d bindings", len(lines), len(Keys))
	}
	for _, k := range Keys {
		found := false
		for _, line := range lines {
			if strings.Contains(line, k.Help) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("help does not mention %q", k.Help)
		}
	}
}

func TestCommandNames(t *testing.T) {
	if CommandNone.String() != "none" {
		t.Error("CommandNone should be named")
	}
	for _, k := range Keys {
		if k.Command.String() == "none" {
			t.Errorf("command for key %q has no name", k.Key)
		}
	}
}

func BenchmarkDraw(b *testing.B) {
	dst := vt.NewGrid(200, 60, 0)
	theme := DefaultTheme()

	screen := vt.NewScreen(98, 28, 0)
	screen.Write([]byte(strings.Repeat("\x1b[32mstatus\x1b[0m output line here\r\n", 28)))

	frame := Frame{Session: "bench"}
	for i := 0; i < 4; i++ {
		frame.Panes = append(frame.Panes, Pane{
			ID:      uint64(i + 1),
			Rect:    Rect{X: (i % 2) * 100, Y: (i / 2) * 30, Cols: 100, Rows: 29},
			Screen:  screen,
			Running: true,
			Focused: i == 0,
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		Draw(dst, frame, theme)
	}
}

// --- mouse -----------------------------------------------------------------

func TestParseMouseReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want MouseEvent
	}{
		{"press", "\x1b[<0;10;5M", MouseEvent{Kind: MousePress, X: 9, Y: 4}},
		{"release", "\x1b[<0;10;5m", MouseEvent{Kind: MouseRelease, X: 9, Y: 4}},
		{"right button", "\x1b[<2;3;3M", MouseEvent{Kind: MousePress, X: 2, Y: 2, Button: 2}},
		{"drag", "\x1b[<32;7;9M", MouseEvent{Kind: MouseDrag, X: 6, Y: 8}},
		{"wheel up", "\x1b[<64;1;1M", MouseEvent{Kind: MouseWheelUp}},
		{"wheel down", "\x1b[<65;1;1M", MouseEvent{Kind: MouseWheelDown, Button: 1}},
		// Coordinates past 223 are exactly why the SGR encoding is asked for.
		{"far right", "\x1b[<0;400;200M", MouseEvent{Kind: MousePress, X: 399, Y: 199}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n, incomplete := parseMouse([]byte(c.in))
			if incomplete || n != len(c.in) {
				t.Fatalf("consumed %d of %d (incomplete=%v)", n, len(c.in), incomplete)
			}
			if got.Kind != c.want.Kind || got.X != c.want.X ||
				got.Y != c.want.Y || got.Button != c.want.Button {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
			// Raw is what gets forwarded to a pane, so it has to be the whole
			// sequence and nothing else.
			if string(got.Raw) != c.in {
				t.Errorf("Raw = %q, want %q", got.Raw, c.in)
			}
		})
	}
}

// TestParseMouseIncomplete: a report split across reads must be waited for,
// not forwarded in halves.
func TestParseMouseIncomplete(t *testing.T) {
	for _, partial := range []string{"\x1b", "\x1b[", "\x1b[<", "\x1b[<0", "\x1b[<0;10;"} {
		_, n, incomplete := parseMouse([]byte(partial))
		if n != 0 || !incomplete {
			t.Errorf("%q: n=%d incomplete=%v, want it to wait", partial, n, incomplete)
		}
	}
}

func TestParseMouseRejectsOtherSequences(t *testing.T) {
	for _, other := range []string{"\x1b[A", "\x1b[1;5A", "hello", "\x1b[<abcM"} {
		_, n, incomplete := parseMouse([]byte(other))
		if n != 0 || incomplete {
			t.Errorf("%q: n=%d incomplete=%v, want it rejected", other, n, incomplete)
		}
	}
}

func TestInputSeparatesMouseFromKeys(t *testing.T) {
	var in Input
	forward, commands, mice := in.FeedAll([]byte("ab\x1b[<0;5;5Mcd"))
	if string(forward) != "abcd" {
		t.Errorf("forwarded %q, want abcd", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v", commands)
	}
	if len(mice) != 1 || mice[0].Kind != MousePress {
		t.Errorf("mice = %+v, want one press", mice)
	}
}

// TestInputBuffersASplitMouseReport: a terminal can deliver one across two
// reads, and half of it reaching a pane would appear there as typed text.
func TestInputBuffersASplitMouseReport(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("\x1b[<0;12"))
	if len(forward) != 0 || len(mice) != 0 {
		t.Fatalf("a partial report produced forward=%q mice=%v", forward, mice)
	}
	forward, _, mice = in.FeedAll([]byte(";7M"))
	if len(forward) != 0 {
		t.Errorf("forwarded %q, want nothing", forward)
	}
	if len(mice) != 1 || mice[0].X != 11 || mice[0].Y != 6 {
		t.Errorf("mice = %+v, want one press at (11,6)", mice)
	}
}

// TestInputGivesUpOnAnEndlessPartial: something that starts like a mouse
// report but never ends must not swallow input forever.
func TestInputGivesUpOnAnEndlessPartial(t *testing.T) {
	var in Input
	long := "\x1b[<" + strings.Repeat("1", maxPartialMouse+10)
	forward, _, _ := in.FeedAll([]byte(long))
	if len(forward) == 0 {
		t.Error("an over-long partial should be released as ordinary input")
	}
}

func TestMouseEnableAndDisableArePaired(t *testing.T) {
	// Whatever is turned on must be turned off, or every later click in that
	// terminal emits gibberish.
	for _, mode := range []string{"1002", "1006"} {
		if !strings.Contains(EnableMouse, mode+"h") {
			t.Errorf("EnableMouse does not set %s", mode)
		}
		if !strings.Contains(DisableMouse, mode+"l") {
			t.Errorf("DisableMouse does not clear %s", mode)
		}
	}
}
