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

	// Nothing of the pane, that is. The handle that brings the sidebar back
	// is always in the corner, or a sidebar put away could not be recovered.
	if got := dst.Line(0).Text(); got != "»" {
		t.Errorf("row 0 = %q, want only the sidebar handle", got)
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

	x, y, visible := CursorPosition(f, 80, 24)
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
	if _, _, visible := CursorPosition(f, 80, 24); visible {
		t.Error("an exited pane should not show a cursor")
	}
}

func TestCursorHiddenWithNoFocus(t *testing.T) {
	if _, _, visible := CursorPosition(Frame{}, 80, 24); visible {
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
		if len(commands) != 1 || commands[0].Command != want {
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
		if len(commands) != 1 || commands[0].Command != want {
			t.Errorf("prefix %q gave %v, want %v", seq, commands, want)
		}
	}
}

// TestInputArrowKeysArrivingApart: the three bytes of an arrow key can be read
// in separate chunks, and the state machine has to survive that.
func TestInputArrowKeysArrivingApart(t *testing.T) {
	var in Input
	var commands []Action
	for _, chunk := range [][]byte{{Prefix}, {0x1b}, {'['}, {'C'}} {
		_, cmds, _ := in.FeedAll(chunk)
		commands = append(commands, cmds...)
	}
	if len(commands) != 1 || commands[0].Command != CommandFocusRight {
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
	if len(commands) != 1 || commands[0].Command != CommandLiteralPrefix {
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
	if len(commands) != 1 || commands[0].Command != CommandClosePane {
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

// TestInputSelectTabCarriesTheNumber: the digit keys all map to one command,
// so the number has to travel with it rather than being encoded in a dozen
// commands that differ only by a value.
func TestInputSelectTabCarriesTheNumber(t *testing.T) {
	for n := 1; n <= 9; n++ {
		var in Input
		_, commands, _ := in.FeedAll([]byte{Prefix, byte('0' + n)})
		if len(commands) != 1 {
			t.Fatalf("digit %d gave %v", n, commands)
		}
		if commands[0].Command != CommandSelectTab || commands[0].Arg != n {
			t.Errorf("digit %d = %+v, want select-tab with arg %d", n, commands[0], n)
		}
	}

	// Zero is not a tab: tabs are counted from one, and a key that quietly
	// selects nothing is worse than one that does nothing.
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, '0'})
	if len(commands) != 0 {
		t.Errorf("zero gave %v, want no command", commands)
	}
	if string(forward) != "0" {
		t.Errorf("zero forwarded %q, want it passed to the pane", forward)
	}
}

func TestInputSpaceAndAgentCommands(t *testing.T) {
	cases := map[byte]Command{
		's': CommandNewSpace,
		')': CommandNextSpace,
		'(': CommandPrevSpace,
		'a': CommandToggleAgents,
		'g': CommandNavigate,
	}
	for key, want := range cases {
		var in Input
		_, commands, _ := in.FeedAll([]byte{Prefix, key})
		if len(commands) != 1 || commands[0].Command != want {
			t.Errorf("key %q gave %v, want %v", string(key), commands, want)
		}
	}
}

// TestInputDeliversALoneEscape is why a lone escape is never held as a
// possible mouse report: inside an editor, a key that does not arrive until
// the next keystroke is indistinguishable from one that was eaten.
func TestInputDeliversALoneEscape(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("abc\x1b"))
	if string(forward) != "abc\x1b" {
		t.Errorf("forwarded %q, want the escape delivered with the rest", forward)
	}
	if len(mice) != 0 {
		t.Errorf("mice = %v", mice)
	}

	// On its own, too.
	var lone Input
	forward, _, _ = lone.FeedAll([]byte{0x1b})
	if len(forward) != 1 || forward[0] != 0x1b {
		t.Errorf("forwarded %q, want a single escape", forward)
	}
}

// TestInputStillWaitsForALikelyMouseReport keeps the fix above from throwing
// away the buffering it was narrowing.
func TestInputStillWaitsForALikelyMouseReport(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("\x1b["))
	if len(forward) != 0 || len(mice) != 0 {
		t.Fatalf("forward=%q mice=%v, want it held", forward, mice)
	}
	forward, _, mice = in.FeedAll([]byte("<0;4;2M"))
	if len(forward) != 0 {
		t.Errorf("forwarded %q, want nothing", forward)
	}
	if len(mice) != 1 || mice[0].X != 3 || mice[0].Y != 1 {
		t.Errorf("mice = %+v", mice)
	}
}

// TestInputReleasesAHeldSequenceThatIsNotAMouseReport: an arrow key begins the
// same way and must still reach the pane.
func TestInputReleasesAHeldSequenceThatIsNotAMouseReport(t *testing.T) {
	var in Input
	if forward, _, _ := in.FeedAll([]byte("\x1b[")); len(forward) != 0 {
		t.Fatalf("forwarded %q too early", forward)
	}
	forward, _, mice := in.FeedAll([]byte("A"))
	if string(forward) != "\x1b[A" {
		t.Errorf("forwarded %q, want the whole arrow key", forward)
	}
	if len(mice) != 0 {
		t.Errorf("mice = %v", mice)
	}
}

// sidebarFrame is a frame showing the sidebar and nothing else, which is what
// the sidebar tests are about.
func sidebarFrame(spaces, agents []SidebarRow) Frame {
	return Frame{
		Sidebar: true,
		Spaces:  SidebarSection{Rows: spaces},
		Agents:  SidebarSection{Rows: agents},
	}
}

func spaceRows(n int) []SidebarRow {
	rows := []SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}
	for i := 0; i < n; i++ {
		rows = append(rows, SidebarRow{
			Kind: SidebarSpace, Label: "space " + itoa(uint64(i+1)),
			Detail: "master", Workspace: uint64(i + 1),
		})
	}
	return append(rows, SidebarRow{Kind: SidebarAction, Label: "new", Action: ActionNewSpace})
}

// TestSidebarDrawsBothSections: the sidebar answers two questions — where
// else could I be, and which agent needs me — and they are separate lists.
func TestSidebarDrawsBothSections(t *testing.T) {
	g := vt.NewGrid(60, 16, 0)
	Draw(g, sidebarFrame(
		[]SidebarRow{
			{Kind: SidebarHeading, Label: "spaces"},
			{Kind: SidebarSpace, Label: "herdr", Detail: "master", Workspace: 1, Active: true},
			{Kind: SidebarAction, Label: "new", Action: ActionNewSpace},
		},
		[]SidebarRow{
			{Kind: SidebarHeading, Label: "agents", Trailing: "flat", TrailingAction: ActionToggleGrouped},
			{Kind: SidebarAgent, Label: "herdr · tab 1", Detail: "claude", Pane: 7, Running: true},
		},
	), DefaultTheme())

	lines := gridText(g)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"spaces", "herdr", "master", "new", "agents", "flat", "claude"} {
		if !strings.Contains(text, want) {
			t.Errorf("the sidebar should show %q:\n%s", want, text)
		}
	}
	// The branch belongs under its space, not beside it.
	if !strings.Contains(lines[1], "herdr") || !strings.Contains(lines[2], "master") {
		t.Errorf("the detail should be drawn beneath the name:\n%s", text)
	}
	// And a rule divides the two, so they do not read as one list.
	if !strings.Contains(text, "─") {
		t.Errorf("the lists should be divided:\n%s", text)
	}
}

// TestSidebarTwoLineEntryIsOneTarget: clicking a branch selects the space it
// belongs to, because that is what it looks like it should do.
func TestSidebarTwoLineEntryIsOneTarget(t *testing.T) {
	f := sidebarFrame([]SidebarRow{
		{Kind: SidebarHeading, Label: "spaces"},
		{Kind: SidebarSpace, Label: "one", Detail: "master", Workspace: 1},
		{Kind: SidebarSpace, Label: "two", Detail: "topic", Workspace: 2},
	}, nil)
	const rows = 20

	cases := map[int]uint64{0: 0, 1: 1, 2: 1, 3: 2, 4: 2}
	for y, want := range cases {
		row, ok := SidebarRowAt(f, 2, y, rows)
		if !ok {
			t.Errorf("row %d: nothing there", y)
			continue
		}
		if row.Workspace != want {
			t.Errorf("row %d selects workspace %d, want %d", y, row.Workspace, want)
		}
	}
	if _, ok := SidebarRowAt(f, SidebarWidth, 1, rows); ok {
		t.Error("past the sidebar belongs to the pane")
	}
}

// TestSidebarHeadingCarriesItsToggle: the toggle sits at the edge of what it
// toggles, and clicking it has to reach it.
func TestSidebarHeadingCarriesItsToggle(t *testing.T) {
	f := sidebarFrame(
		[]SidebarRow{{Kind: SidebarHeading, Label: "spaces"}},
		[]SidebarRow{{
			Kind: SidebarHeading, Label: "agents", Trailing: "grouped",
			Action: ActionToggleGrouped, TrailingAction: ActionToggleGrouped,
		}},
	)
	const rows = 20
	_, agents := SidebarRegions(f, rows)

	row, ok := SidebarRowAt(f, SidebarWidth-4, agents.Y, rows)
	if !ok || row.Action != ActionToggleGrouped {
		t.Errorf("the toggle should be clickable, got %+v ok=%v", row, ok)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	line := gridText(g)[agents.Y]
	if at := strings.Index(line, "grouped"); at < strings.Index(line, "agents")+len("agents") {
		t.Errorf("the toggle should sit at the right edge:\n%q", line)
	}
}

// TestSidebarHiddenTakesNoColumns: turning it off has to give the columns back
// rather than leaving a blank margin.
func TestSidebarHiddenTakesNoColumns(t *testing.T) {
	g := vt.NewGrid(40, 6, 0)
	Draw(g, Frame{Spaces: SidebarSection{Rows: []SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}}}, DefaultTheme())
	if text := strings.Join(gridText(g), "\n"); strings.Contains(text, "spaces") {
		t.Errorf("a hidden sidebar should draw nothing:\n%s", text)
	}
	if _, ok := SidebarRowAt(Frame{}, 0, 0, 40); ok {
		t.Error("a hidden sidebar should have no targets")
	}
}

// TestSidebarTrailingIsItsOwnTarget: the "menu" button sits on the "new" row,
// and clicking it must not create a space.
func TestSidebarTrailingIsItsOwnTarget(t *testing.T) {
	f := sidebarFrame([]SidebarRow{{
		Kind: SidebarAction, Label: "new", Action: ActionNewSpace,
		Trailing: "menu", TrailingAction: ActionOpenMenu,
	}}, nil)
	const rows = 20

	if row, _ := SidebarRowAt(f, 2, 0, rows); row.Action != ActionNewSpace {
		t.Errorf("the left of the row should create a space, got %q", row.Action)
	}
	at := TrailingStart(f.Spaces.Rows[0])
	if at < 0 {
		t.Fatal("the trailing button should have a column")
	}
	if row, _ := SidebarRowAt(f, at, 0, rows); row.Action != ActionOpenMenu {
		t.Errorf("the button should open the menu, got %q", row.Action)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[0]; !strings.Contains(line, "new") || !strings.Contains(line, "menu") {
		t.Errorf("both should be drawn on one row:\n%q", line)
	}
}

// TestSidebarDrawsAFoldedGroup: the triangle points at what it will do, and a
// folded group shows nothing of what is inside it except that it is waiting.
func TestSidebarDrawsAFoldedGroup(t *testing.T) {
	open := sidebarFrame([]SidebarRow{
		{Kind: SidebarSpaceGroup, Label: "clients", Group: "clients", Action: ActionToggleGroup},
		{Kind: SidebarSpace, Label: "backend", Detail: "develop", Depth: 1, Workspace: 1},
	}, nil)
	g := vt.NewGrid(40, 20, 0)
	Draw(g, open, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[0], "▼ clients") {
		t.Errorf("an open group points down:\n%q", lines[0])
	}
	if strings.Index(lines[1], "backend") <= strings.Index(lines[0], "clients") {
		t.Errorf("the member should be indented:\n%q\n%q", lines[0], lines[1])
	}

	folded := sidebarFrame([]SidebarRow{{
		Kind: SidebarSpaceGroup, Label: "clients", Group: "clients",
		Folded: true, Trailing: "2 waiting", Action: ActionToggleGroup,
	}}, nil)
	g = vt.NewGrid(40, 20, 0)
	Draw(g, folded, DefaultTheme())
	line := gridText(g)[0]
	if !strings.Contains(line, "▶ clients") {
		t.Errorf("a folded group points right:\n%q", line)
	}
	// Folding is not a way to stop being told an agent is waiting.
	if !strings.Contains(line, "2 waiting") {
		t.Errorf("a folded group should still report its members:\n%q", line)
	}

	row, ok := SidebarRowAt(folded, 3, 0, 20)
	if !ok || row.Action != ActionToggleGroup || row.Group != "clients" {
		t.Errorf("heading target = %+v ok=%v", row, ok)
	}
}

// TestGroupMenuActsOnTheGroup: a group has no record of its own, so its menu
// carries the name rather than an identifier.
func TestGroupMenuActsOnTheGroup(t *testing.T) {
	m := GroupMenu("clients", false, 0, 0)
	if m.Group != "clients" || m.Workspace != 0 {
		t.Errorf("group menu targets %+v", m)
	}
	if m.Items[0].Label != "fold" {
		t.Errorf("an open group offers to fold, got %q", m.Items[0].Label)
	}
	if shut := GroupMenu("clients", true, 0, 0); shut.Items[0].Label != "unfold" {
		t.Errorf("a folded group offers to unfold, got %q", shut.Items[0].Label)
	}
}

// --- the divided sidebar ---------------------------------------------------

// TestSidebarSectionsAreSeparate is the bug this was found by: the spaces
// pushed the agents off the bottom, so the list that matters most was the one
// you could not see.
func TestSidebarSectionsAreSeparate(t *testing.T) {
	f := sidebarFrame(spaceRows(9), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	const rows = 20

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "agents") || !strings.Contains(text, "claude") {
		t.Errorf("a long list of spaces must not hide the agents:\n%s", text)
	}
	if !strings.Contains(text, "spaces") {
		t.Errorf("both headings stay on screen:\n%s", text)
	}
}

// TestSidebarHeadingsStayPut: a list whose title scrolls away leaves names
// with nothing saying what they are names of, and takes the toggle with it.
func TestSidebarHeadingsStayPut(t *testing.T) {
	f := sidebarFrame(spaceRows(9), nil)
	const rows = 20
	spaces, _ := SidebarRegions(f, rows)

	f.Spaces.Scroll = SidebarMaxScroll(f.Spaces, spaces.Rows)
	if f.Spaces.Scroll == 0 {
		t.Fatal("this list should be scrollable")
	}
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[spaces.Y], "spaces") {
		t.Errorf("the heading should stay at the top of its section:\n%q", lines[spaces.Y])
	}
	// And it is still the click target it was.
	if row, ok := SidebarRowAt(f, 2, spaces.Y, rows); !ok || row.Kind != SidebarHeading {
		t.Errorf("the pinned heading should still be hit-testable, got %+v ok=%v", row, ok)
	}
}

// TestSidebarDividerIsGrabbable: the line between the lists is a handle, and
// hit-testing has to find it where it is drawn.
func TestSidebarDividerIsGrabbable(t *testing.T) {
	f := sidebarFrame(spaceRows(3), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	const rows = 20

	at := SidebarSplitAt(f, rows)
	if got := SidebarPlaceAt(f, 2, at, rows); got != SidebarDivider {
		t.Errorf("the divider is at %d, hit-test says %v", at, got)
	}
	if got := SidebarPlaceAt(f, 2, at-1, rows); got != SidebarSpacesList {
		t.Errorf("above the divider is the spaces list, got %v", got)
	}
	if got := SidebarPlaceAt(f, 2, at+1, rows); got != SidebarAgentsList {
		t.Errorf("below the divider is the agents list, got %v", got)
	}
	if got := SidebarPlaceAt(f, SidebarWidth, at, rows); got != SidebarNowhere {
		t.Errorf("past the sidebar is nowhere, got %v", got)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[at]; !strings.Contains(line, "─") {
		t.Errorf("the divider should be drawn where it is grabbed:\n%q", line)
	}
}

// TestSidebarSplitIsClamped: a drag past either end stops where the divider
// can go, rather than storing a position it cannot.
func TestSidebarSplitIsClamped(t *testing.T) {
	f := sidebarFrame(spaceRows(3), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	const rows = 20
	height := SidebarHeight(rows)

	f.SidebarSplit = -50
	if at := SidebarSplitAt(f, rows); at < 1 || at >= height {
		t.Errorf("dragged off the top = %d, out of 0..%d", at, height)
	}
	f.SidebarSplit = 500
	high := SidebarSplitAt(f, rows)
	if high >= height {
		// There must be room left for the list below it.
		_, agents := SidebarRegions(f, rows)
		if agents.Rows <= 0 {
			t.Errorf("dragged off the bottom leaves no agents list: split=%d", high)
		}
	}

	// A terminal too short to divide gives the space to one list rather than
	// drawing a divider with nothing either side of it.
	tiny := sidebarFrame(spaceRows(1), nil)
	if _, agents := SidebarRegions(tiny, 4); agents.Rows != 0 {
		t.Errorf("no room to divide should leave one list, got %+v", agents)
	}
}

// TestSidebarScrollsEachSectionOnItsOwn: one list filling up must not push the
// other out of sight, which is the whole reason they are divided.
func TestSidebarScrollsEachSectionOnItsOwn(t *testing.T) {
	f := sidebarFrame(spaceRows(9), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	const rows = 20
	spaces, agents := SidebarRegions(f, rows)

	if SidebarMaxScroll(f.Spaces, spaces.Rows) == 0 {
		t.Fatal("nine spaces should not fit their section")
	}
	if got := SidebarMaxScroll(f.Agents, agents.Rows); got != 0 {
		t.Errorf("one agent fits, so its list should not scroll: %d", got)
	}

	// Scrolling the spaces does not move the agents.
	f.Spaces.Scroll = 3
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if !strings.Contains(strings.Join(gridText(g), "\n"), "claude") {
		t.Error("scrolling one list should leave the other where it was")
	}

	// Hit-testing follows the scroll: the first entry under the heading is
	// the one the offset put there.
	row, ok := SidebarRowAt(f, 2, spaces.Y+1, rows)
	if !ok || row.Workspace != 4 {
		t.Errorf("scrolled by three, the first space shown is 4, got %+v", row)
	}
}

// TestSidebarRevealMovesAsLittleAsPossible: jumping to another space should
// move the list only as far as it must, so what surrounds the entry being left
// stays where the eye last saw it.
func TestSidebarRevealMovesAsLittleAsPossible(t *testing.T) {
	f := sidebarFrame(spaceRows(9), nil)
	const rows = 20
	spaces, _ := SidebarRegions(f, rows)

	if got := SidebarRevealScroll(f.Spaces, spaces.Rows, 2); got != 0 {
		t.Errorf("reveal of a visible entry = %d, want 0", got)
	}
	last := len(f.Spaces.Rows) - 1
	at := SidebarRevealScroll(f.Spaces, spaces.Rows, last)
	if at == 0 || at > SidebarMaxScroll(f.Spaces, spaces.Rows) {
		t.Errorf("reveal of the last entry = %d, max %d", at, SidebarMaxScroll(f.Spaces, spaces.Rows))
	}
	f.Spaces.Scroll = at
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if !strings.Contains(strings.Join(gridText(g), "\n"), "new") {
		t.Error("the revealed entry should be on screen")
	}

	// The heading is pinned, so revealing it moves nothing.
	if got := SidebarRevealScroll(f.Spaces, spaces.Rows, 0); got != at {
		t.Errorf("revealing a pinned row moved the list from %d to %d", at, got)
	}
}

// TestSidebarActiveRowPrefersTheCursor: while the list has the keyboard, what
// must stay in view is where the cursor is, not where the user came from.
func TestSidebarActiveRowPrefersTheCursor(t *testing.T) {
	s := SidebarSection{Rows: spaceRows(3)}
	s.Rows[1].Active = true
	s.Rows[3].Selected = true
	if got := SidebarActiveRow(s); got != 3 {
		t.Errorf("active row = %d, want the selected one", got)
	}

	s.Rows[3].Selected = false
	if got := SidebarActiveRow(s); got != 1 {
		t.Errorf("active row = %d, want the current space", got)
	}
	if got := SidebarActiveRow(SidebarSection{Rows: spaceRows(3)}); got != -1 {
		t.Errorf("with nothing current the answer is none, got %d", got)
	}
}

// TestMenuStaysOnTheScreen: a menu opened near the bottom edge would lose the
// items added last, which are the destructive ones.
func TestMenuStaysOnTheScreen(t *testing.T) {
	m := PaneMenu(1, 78, 19, true)
	r := m.Rect(80, 20)
	if r.X+r.Cols > 80 || r.Y+r.Rows > 20 {
		t.Errorf("menu at %+v runs off an 80x20 screen", r)
	}
	if r.Rows != len(m.Items)+2 {
		t.Errorf("menu should show every item: rows=%d items=%d", r.Rows, len(m.Items))
	}

	// On a screen too small for it, it is clipped rather than placed outside.
	small := m.Rect(10, 3)
	if small.X != 0 || small.Y != 0 || small.Cols > 10 || small.Rows > 3 {
		t.Errorf("menu on a tiny screen: %+v", small)
	}
}

// TestMenuHitTestSeparatesInsideFromOnAnItem: a click inside the border but
// not on an item must not fall through, or one gesture would close the menu
// and act on whatever it was covering.
func TestMenuHitTestSeparatesInsideFromOnAnItem(t *testing.T) {
	m := TabMenu(4, 10, 5)
	r := m.Rect(80, 24)

	if _, onItem, inside := MenuItemAt(m, 80, 24, r.X, r.Y); onItem || !inside {
		t.Errorf("the border is inside but not an item: onItem=%v inside=%v", onItem, inside)
	}
	item, onItem, inside := MenuItemAt(m, 80, 24, r.X+2, r.Y+1)
	if !onItem || !inside || item.Action != MenuNewTab {
		t.Errorf("first item = %+v onItem=%v inside=%v", item, onItem, inside)
	}
	last, onItem, _ := MenuItemAt(m, 80, 24, r.X+2, r.Y+len(m.Items))
	if !onItem || last.Action != MenuClose {
		t.Errorf("last item = %+v onItem=%v", last, onItem)
	}
	if _, _, inside := MenuItemAt(m, 80, 24, r.X-1, r.Y); inside {
		t.Error("a point outside the border is outside the menu")
	}
}

// TestMenuTargetsWhatItWasOpenedOn is the whole reason a menu is better than
// a key here: there is no current selection to be wrong about.
func TestMenuTargetsWhatItWasOpenedOn(t *testing.T) {
	if m := PaneMenu(7, 0, 0, true); m.Pane != 7 || m.Tab != 0 || m.Workspace != 0 {
		t.Errorf("pane menu targets %+v", m)
	}
	if m := SpaceMenu(3, 0, 0); m.Workspace != 3 || m.Pane != 0 {
		t.Errorf("space menu targets %+v", m)
	}

	// The last pane in a tab offers no "close pane": closing it would leave an
	// empty tab, and "close tab" is the honest name for that.
	alone := PaneMenu(1, 0, 0, false)
	for _, item := range alone.Items {
		if item.Action == MenuClose {
			t.Error("a lone pane should not offer to close itself")
		}
	}
}

// TestTabBarStartsWhereThePanesDo: the tabs belong to one space, so a bar
// running over the sidebar would read as though they belonged to the session.
func TestTabBarStartsWhereThePanesDo(t *testing.T) {
	f := sidebarFrame([]SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}, nil)
	f.Tabs = []Tab{{ID: 1, Name: "tab 1", Active: true}}

	g := vt.NewGrid(60, 8, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)

	if at := strings.Index(lines[0], "tab 1"); at < SidebarWidth {
		t.Errorf("the bar should start past the sidebar, found at %d:\n%q", at, lines[0])
	}
	// The sidebar owns its own top row rather than starting below the bar.
	if !strings.Contains(lines[0], "spaces") {
		t.Errorf("the sidebar should run from the top:\n%q", lines[0])
	}

	// A click on the sidebar's top row is not a click on a tab.
	if _, _, ok := TabAt(f, 2, 0, 60); ok {
		t.Error("the sidebar's columns should not answer for the tab bar")
	}
	if _, _, ok := TabAt(f, SidebarWidth+2, 0, 60); !ok {
		t.Error("the first tab should be clickable where it is drawn")
	}

	// With no sidebar the bar starts after the gutter kept for the handle
	// that brings it back, and not before it.
	plain := Frame{Tabs: f.Tabs}
	if segs := TabSegments(plain, 60); len(segs) == 0 || segs[0].Start != ShowHandleWidth {
		t.Errorf("without a sidebar the bar should start past the handle: %+v", segs)
	}
}

// TestStatusSaysHowManyAreWaiting: the whole reason to run tend is that the
// agent needing you is usually not the one on screen, and the list can be
// folded, scrolled, or turned off.
func TestStatusSaysHowManyAreWaiting(t *testing.T) {
	g := vt.NewGrid(60, 6, 0)
	Draw(g, Frame{Session: "work", Waiting: 2}, DefaultTheme())
	last := gridText(g)[5]
	if !strings.Contains(last, "2 waiting") {
		t.Errorf("the status bar should say what is waiting:\n%q", last)
	}

	g = vt.NewGrid(60, 6, 0)
	Draw(g, Frame{Session: "work"}, DefaultTheme())
	if last := gridText(g)[5]; strings.Contains(last, "waiting") {
		t.Errorf("with nothing waiting it should say nothing:\n%q", last)
	}
}

// TestSidebarHandleTogglesFromEitherSide: a way out that is only a keystroke
// is one that somebody who arrived by mouse cannot find again.
func TestSidebarHandleTogglesFromEitherSide(t *testing.T) {
	const rows = 20
	shown := sidebarFrame(spaceRows(2), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})

	g := vt.NewGrid(60, rows, 0)
	Draw(g, shown, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "«") {
		t.Errorf("a shown sidebar offers to hide itself:\n%s", text)
	}
	if !SidebarHandleAt(shown, SidebarWidth-2, SidebarHeight(rows)-1, rows) {
		t.Error("the handle should be where it is drawn")
	}
	if SidebarHandleAt(shown, 2, 2, rows) {
		t.Error("the rest of the list is not the handle")
	}

	hidden := Frame{Tabs: []Tab{{ID: 1, Name: "tab 1"}}}
	g = vt.NewGrid(60, rows, 0)
	Draw(g, hidden, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[0], "»") {
		t.Errorf("a hidden sidebar offers to come back:\n%q", lines[0])
	}
	if !SidebarHandleAt(hidden, 0, 0, rows) {
		t.Error("the handle should answer where it is drawn")
	}

	// The gutter is kept for it, so the tab bar does not run over it.
	if got := SidebarGutter(hidden, 60); got != ShowHandleWidth {
		t.Errorf("hidden gutter = %d, want room for the handle", got)
	}
	if _, _, ok := TabAt(hidden, 0, 0, 60); ok {
		t.Error("the handle's columns are not the tab bar's")
	}
	if at := strings.Index(lines[0], "tab 1"); at < ShowHandleWidth {
		t.Errorf("the tabs should start past the handle:\n%q", lines[0])
	}
}

// TestSidebarFooterStaysAtTheBottom: a button that floats after the last entry
// moves every time the list grows, so the place to reach for it changes with
// something the user did not do.
func TestSidebarFooterStaysAtTheBottom(t *testing.T) {
	const rows = 24
	f := sidebarFrame(spaceRows(2), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	f.Spaces.Footer = []SidebarRow{{
		Kind: SidebarAction, Label: "new", Action: ActionNewSpace,
		Trailing: "menu", TrailingAction: ActionOpenMenu,
	}}
	spaces, _ := SidebarRegions(f, rows)
	foot := spaces.Y + spaces.Rows - 1

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[foot], "new") || !strings.Contains(lines[foot], "menu") {
		t.Errorf("the footer should be on the last line of its section:\n%q", lines[foot])
	}
	// Directly above the divider, not floating after the entries.
	if at := SidebarSplitAt(f, rows); foot != at-1 {
		t.Errorf("footer at %d, divider at %d: they should touch", foot, at)
	}

	// A longer list does not move it.
	f.Spaces.Rows = spaceRows(12)
	g = vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[foot]; !strings.Contains(line, "new") {
		t.Errorf("the footer moved when the list grew:\n%q", line)
	}

	// And it is clickable where it is drawn, both halves of it.
	if row, ok := SidebarRowAt(f, 2, foot, rows); !ok || row.Action != ActionNewSpace {
		t.Errorf("the footer should be a target, got %+v ok=%v", row, ok)
	}
	at := TrailingStart(f.Spaces.Footer[0])
	if row, ok := SidebarRowAt(f, at, foot, rows); !ok || row.Action != ActionOpenMenu {
		t.Errorf("the footer's button should be its own target, got %+v", row)
	}
}

// TestSidebarGivesTheRoomToSpaces: a space is a place that goes on existing,
// an agent is what happens to be running, and there are rarely many at once.
func TestSidebarGivesTheRoomToSpaces(t *testing.T) {
	const rows = 30
	f := sidebarFrame(spaceRows(12), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	spaces, agents := SidebarRegions(f, rows)

	if spaces.Rows <= agents.Rows {
		t.Errorf("spaces got %d lines, agents %d: the spaces should have the room",
			spaces.Rows, agents.Rows)
	}
	if agents.Rows < sidebarMinSection {
		t.Errorf("the agents list should keep a usable strip, got %d", agents.Rows)
	}
	// And the default is a default: a drag still decides.
	f.SidebarSplit = 6
	if got := SidebarSplitAt(f, rows); got != 6 {
		t.Errorf("a dragged split should be kept, got %d", got)
	}
}

// TestSidebarKeepsRoomForAgentsThatHaveNotArrived: sizing the strip to what is
// in it means an empty one is three lines and the first agent to appear has
// nowhere to appear.
func TestSidebarKeepsRoomForAgentsThatHaveNotArrived(t *testing.T) {
	const rows = 30
	empty := sidebarFrame(spaceRows(12), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	_, agents := SidebarRegions(empty, rows)

	if agents.Rows <= sectionHeight(empty.Agents) {
		t.Errorf("an empty list got %d lines for %d of content: it should keep room",
			agents.Rows, sectionHeight(empty.Agents))
	}
	// But not so much that the spaces suffer for it.
	spaces, _ := SidebarRegions(empty, rows)
	if spaces.Rows <= agents.Rows {
		t.Errorf("spaces %d, agents %d: the spaces still get the room", spaces.Rows, agents.Rows)
	}

	// A full list is capped rather than taking the column.
	full := sidebarFrame(spaceRows(2), append(
		[]SidebarRow{{Kind: SidebarHeading, Label: "agents"}},
		spaceRows(20)...,
	))
	_, many := SidebarRegions(full, rows)
	if many.Rows > rows*2/5 {
		t.Errorf("a long list of agents took %d of %d lines", many.Rows, rows)
	}
}
