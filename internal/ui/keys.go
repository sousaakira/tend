package ui

// Key handling is a state machine over raw bytes rather than a parsed key
// event, because almost every byte belongs to the focused pane and must reach
// it untouched. Only the prefix key and the one command key after it are the
// client's; everything else is forwarded exactly as typed, including escape
// sequences the client has never heard of.

// Prefix is the key that arms a command. Ctrl+B, as in tmux and screen, since
// anyone reaching for a multiplexer already has it in their fingers.
const Prefix = 0x02

// Command is what a prefix key sequence asked for.
type Command uint8

const (
	// CommandNone means nothing was triggered.
	CommandNone Command = iota
	CommandSplitColumns
	CommandSplitRows
	CommandFocusLeft
	CommandFocusRight
	CommandFocusUp
	CommandFocusDown
	CommandFocusNext
	CommandClosePane
	CommandZoom
	CommandGrowLeft
	CommandGrowRight
	CommandGrowUp
	CommandGrowDown
	CommandNewTab
	CommandNextTab
	CommandPrevTab
	CommandDetach
	CommandRefresh
	CommandHelp
	// CommandLiteralPrefix sends the prefix key itself to the pane, which is
	// how an inner multiplexer or an editor bound to Ctrl+B still receives it.
	CommandLiteralPrefix
)

func (c Command) String() string {
	switch c {
	case CommandSplitColumns:
		return "split-columns"
	case CommandSplitRows:
		return "split-rows"
	case CommandFocusLeft:
		return "focus-left"
	case CommandFocusRight:
		return "focus-right"
	case CommandFocusUp:
		return "focus-up"
	case CommandFocusDown:
		return "focus-down"
	case CommandFocusNext:
		return "focus-next"
	case CommandClosePane:
		return "close-pane"
	case CommandZoom:
		return "zoom"
	case CommandGrowLeft:
		return "grow-left"
	case CommandGrowRight:
		return "grow-right"
	case CommandGrowUp:
		return "grow-up"
	case CommandGrowDown:
		return "grow-down"
	case CommandNewTab:
		return "new-tab"
	case CommandNextTab:
		return "next-tab"
	case CommandPrevTab:
		return "prev-tab"
	case CommandDetach:
		return "detach"
	case CommandRefresh:
		return "refresh"
	case CommandHelp:
		return "help"
	case CommandLiteralPrefix:
		return "literal-prefix"
	default:
		return "none"
	}
}

// Keys is the bindings, listed once so the help text cannot drift from what
// the keys actually do.
var Keys = []struct {
	Key     string
	Command Command
	Help    string
}{
	{"|", CommandSplitColumns, "split beside"},
	{"-", CommandSplitRows, "split below"},
	{"h ←", CommandFocusLeft, "focus left"},
	{"l →", CommandFocusRight, "focus right"},
	{"k ↑", CommandFocusUp, "focus up"},
	{"j ↓", CommandFocusDown, "focus down"},
	{"o", CommandFocusNext, "focus next"},
	{"x", CommandClosePane, "close pane"},
	{"z", CommandZoom, "zoom pane"},
	{"HJKL", CommandGrowRight, "resize pane"},
	{"c", CommandNewTab, "new tab"},
	{"n", CommandNextTab, "next tab"},
	{"p", CommandPrevTab, "previous tab"},
	{"d", CommandDetach, "detach"},
	{"r", CommandRefresh, "redraw"},
	{"?", CommandHelp, "this help"},
}

// Input turns a byte stream into pane input and client commands.
//
// The zero value is ready to use. It is not safe for concurrent use; one
// reader feeds it.
type Input struct {
	armed bool
	// pending holds an escape sequence being read after the prefix, so that
	// an arrow key — three bytes that may arrive separately — is recognised
	// rather than half-forwarded.
	pending []byte
}

// Armed reports whether the prefix key is waiting for a command, which the
// status bar shows so the user is never guessing about the mode they are in.
func (in *Input) Armed() bool { return in.armed }

// Result is what one byte produced.
type Result struct {
	// Forward is bytes destined for the focused pane.
	Forward []byte
	// Command is a client action, or CommandNone.
	Command Command
}

// Feed processes one byte.
func (in *Input) Feed(b byte) Result {
	if !in.armed {
		if b == Prefix {
			in.armed = true
			in.pending = in.pending[:0]
			return Result{}
		}
		return Result{Forward: []byte{b}}
	}
	return in.command(b)
}

// FeedAll processes a chunk, returning everything to forward and every command
// in order.
//
// Bytes and commands are kept in order relative to each other, because a chunk
// can hold both: a paste that ends mid-sequence, or a prefix typed fast enough
// to arrive with the key after it.
func (in *Input) FeedAll(data []byte) ([]byte, []Command) {
	var forward []byte
	var commands []Command
	for _, b := range data {
		r := in.Feed(b)
		forward = append(forward, r.Forward...)
		if r.Command != CommandNone {
			commands = append(commands, r.Command)
		}
	}
	return forward, commands
}

// command interprets a byte while the prefix is armed.
func (in *Input) command(b byte) Result {
	// An escape sequence after the prefix is an arrow key. It is collected
	// rather than acted on byte by byte, since its bytes can arrive apart.
	if len(in.pending) > 0 || b == 0x1b {
		in.pending = append(in.pending, b)
		switch {
		case len(in.pending) == 1:
			return Result{} // just ESC so far
		case len(in.pending) == 2:
			if in.pending[1] != '[' {
				// Not an arrow key after all. The prefix is spent and the
				// bytes go to the pane, since guessing would eat a key.
				return in.release()
			}
			return Result{}
		default:
			cmd := arrowCommand(in.pending[2])
			in.disarm()
			if cmd == CommandNone {
				return Result{}
			}
			return Result{Command: cmd}
		}
	}

	in.disarm()

	switch b {
	case '|', '\\', '%':
		return Result{Command: CommandSplitColumns}
	case '-', '"':
		return Result{Command: CommandSplitRows}
	case 'h':
		return Result{Command: CommandFocusLeft}
	case 'l':
		return Result{Command: CommandFocusRight}
	case 'k':
		return Result{Command: CommandFocusUp}
	case 'j':
		return Result{Command: CommandFocusDown}
	case 'o':
		return Result{Command: CommandFocusNext}
	case 'x':
		return Result{Command: CommandClosePane}
	case 'z':
		return Result{Command: CommandZoom}
	// Shifted movement keys resize instead of moving, which is the one
	// convention every multiplexer shares.
	case 'H':
		return Result{Command: CommandGrowLeft}
	case 'L':
		return Result{Command: CommandGrowRight}
	case 'K':
		return Result{Command: CommandGrowUp}
	case 'J':
		return Result{Command: CommandGrowDown}
	case 'c':
		return Result{Command: CommandNewTab}
	case 'n':
		return Result{Command: CommandNextTab}
	case 'p':
		return Result{Command: CommandPrevTab}
	case 'd':
		return Result{Command: CommandDetach}
	case 'r':
		return Result{Command: CommandRefresh}
	case '?':
		return Result{Command: CommandHelp}
	case Prefix:
		return Result{Command: CommandLiteralPrefix, Forward: []byte{Prefix}}
	}

	// An unbound key cancels the prefix and is forwarded, so a mistyped
	// command does not silently swallow the next keystroke.
	return Result{Forward: []byte{b}}
}

func arrowCommand(final byte) Command {
	switch final {
	case 'A':
		return CommandFocusUp
	case 'B':
		return CommandFocusDown
	case 'C':
		return CommandFocusRight
	case 'D':
		return CommandFocusLeft
	}
	return CommandNone
}

// release cancels the prefix and forwards what was collected.
func (in *Input) release() Result {
	out := make([]byte, len(in.pending))
	copy(out, in.pending)
	in.disarm()
	return Result{Forward: out}
}

func (in *Input) disarm() {
	in.armed = false
	in.pending = in.pending[:0]
}

// HelpLines renders the bindings for the help overlay.
func HelpLines() []string {
	lines := make([]string, 0, len(Keys)+1)
	lines = append(lines, "ctrl+b then:")
	for _, k := range Keys {
		lines = append(lines, "  "+pad(k.Key, 5)+" "+k.Help)
	}
	return lines
}

func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}
