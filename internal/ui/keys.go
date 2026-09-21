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
	CommandScroll
	CommandGrowLeft
	CommandGrowRight
	CommandGrowUp
	CommandGrowDown
	CommandNewTab
	CommandNextTab
	CommandPrevTab
	CommandSelectTab
	CommandNextSpace
	CommandPrevSpace
	CommandNewSpace
	CommandToggleAgents
	CommandNavigate
	CommandMenu
	CommandRenameTab
	CommandRenameSpace
	CommandDetach
	CommandRefresh
	CommandHelp
	// CommandSwapLeft and its siblings trade the focused pane with its
	// neighbour on that side, herdr's prefix+shift+h/j/k/l.
	CommandSwapLeft
	CommandSwapRight
	CommandSwapUp
	CommandSwapDown
	// CommandResizeMode enters resize mode, where h/j/k/l move the focused
	// pane's edges until escape — herdr's prefix+r.
	CommandResizeMode
	// CommandFocusPrev goes back through the tab's panes, herdr's
	// prefix+shift+tab; CommandFocusNext is also on prefix+tab.
	CommandFocusPrev
	// CommandLastPane goes back to the pane that was focused before this one,
	// wherever it is.
	CommandLastPane
	// CommandPrevAgent and CommandNextAgent step through the agents in the
	// order the sidebar lists them.
	CommandPrevAgent
	CommandNextAgent
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
	case CommandScroll:
		return "scroll"
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
	case CommandSelectTab:
		return "select-tab"
	case CommandNextSpace:
		return "next-space"
	case CommandPrevSpace:
		return "prev-space"
	case CommandNewSpace:
		return "new-space"
	case CommandToggleAgents:
		return "toggle-agents"
	case CommandNavigate:
		return "navigate"
	case CommandMenu:
		return "menu"
	case CommandRenameTab:
		return "rename-tab"
	case CommandRenameSpace:
		return "rename-space"
	case CommandDetach:
		return "detach"
	case CommandRefresh:
		return "refresh"
	case CommandHelp:
		return "help"
	case CommandSwapLeft:
		return "swap-left"
	case CommandSwapRight:
		return "swap-right"
	case CommandSwapUp:
		return "swap-up"
	case CommandSwapDown:
		return "swap-down"
	case CommandResizeMode:
		return "resize-mode"
	case CommandFocusPrev:
		return "focus-prev"
	case CommandLastPane:
		return "last-pane"
	case CommandPrevAgent:
		return "prev-agent"
	case CommandNextAgent:
		return "next-agent"
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
	{"o tab", CommandFocusNext, "focus next"},
	{"⇧tab", CommandFocusPrev, "focus previous"},
	{";", CommandLastPane, "last pane"},
	{"< >", CommandNextAgent, "previous / next agent"},
	{"x", CommandClosePane, "close pane"},
	{"z", CommandZoom, "zoom pane"},
	{"[", CommandScroll, "copy mode"},
	{"HJKL", CommandSwapRight, "swap pane"},
	{"r", CommandResizeMode, "resize (hjkl, esc)"},
	{"c", CommandNewTab, "new tab"},
	{"n", CommandNextTab, "next tab"},
	{"p", CommandPrevTab, "previous tab"},
	{"1-9", CommandSelectTab, "go to tab"},
	{"s", CommandNewSpace, "new space"},
	{"( )", CommandNextSpace, "switch space"},
	{"a", CommandToggleAgents, "show agents"},
	{"w g", CommandNavigate, "pick a space or agent"},
	{"m", CommandMenu, "menu"},
	{",", CommandRenameTab, "rename tab"},
	{".", CommandRenameSpace, "rename space"},
	{"d", CommandDetach, "detach"},
	{"R", CommandRefresh, "redraw"},
	{"?", CommandHelp, "this help"},
}

// Input turns a byte stream into pane input and client commands.
//
// The zero value is ready to use. It is not safe for concurrent use; one
// reader feeds it.
type Input struct {
	// PrefixKey overrides the default. Zero uses Prefix; a prefix of none is
	// expressed by setting it to a byte no keyboard produces, which Disabled
	// does.
	PrefixKey byte

	armed bool
	// pending holds an escape sequence being read after the prefix, so that
	// an arrow key — three bytes that may arrive separately — is recognised
	// rather than half-forwarded.
	pending []byte
	// partialMouse holds the beginning of a mouse report split across reads.
	partialMouse []byte
}

// maxPartialMouse bounds how long a suspected mouse report is held. Beyond
// this it was never one, and holding it would swallow real input.
const maxPartialMouse = 32

// Disabled is a prefix that no key produces, for a user who has turned the
// prefix off and drives tend some other way.
const Disabled byte = 0xFF

func (in *Input) prefix() byte {
	if in.PrefixKey == 0 {
		return Prefix
	}
	return in.PrefixKey
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
	// Arg carries a command's argument, such as which tab a digit selected.
	Arg int
}

// Feed processes one byte.
func (in *Input) Feed(b byte) Result {
	if !in.armed {
		if b == in.prefix() {
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
// Action is a command and its argument, kept together so a caller does not
// have to pair them up by position.
type Action struct {
	Command Command
	Arg     int
}

func (in *Input) FeedAll(data []byte) ([]byte, []Action, []MouseEvent) {
	if len(in.partialMouse) > 0 {
		data = append(in.partialMouse, data...)
		in.partialMouse = nil
	}

	var forward []byte
	var commands []Action
	var mice []MouseEvent

	for i := 0; i < len(data); {
		// Mouse reports are read before the key machine sees them: they are
		// not keys, and the prefix has nothing to do with them.
		ev, n, incomplete := parseMouse(data[i:])
		if n > 0 {
			mice = append(mice, ev)
			i += n
			continue
		}
		if incomplete {
			rest := data[i:]
			// A lone escape is never held. It is far more often the Escape
			// key than the start of a mouse report, and holding it means the
			// key does not arrive until the user presses something else —
			// which inside an editor is indistinguishable from tend having
			// eaten it. Two bytes are enough to be worth waiting for.
			if len(rest) >= 2 && len(rest) < maxPartialMouse {
				in.partialMouse = append(in.partialMouse[:0], rest...)
				return forward, commands, mice
			}
		}

		r := in.Feed(data[i])
		forward = append(forward, r.Forward...)
		if r.Command != CommandNone {
			commands = append(commands, Action{Command: r.Command, Arg: r.Arg})
		}
		i++
	}
	return forward, commands, mice
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
	case 'o', '\t':
		return Result{Command: CommandFocusNext}
	case ';':
		return Result{Command: CommandLastPane}
	case '<':
		return Result{Command: CommandPrevAgent}
	case '>':
		return Result{Command: CommandNextAgent}
	case 'x':
		return Result{Command: CommandClosePane}
	case 'z':
		return Result{Command: CommandZoom}
	case '[':
		return Result{Command: CommandScroll}
	// Shifted movement keys swap the pane that way, herdr's binding. Resizing
	// has a mode of its own on r, where the unshifted keys do it repeatedly
	// without the prefix before each press.
	case 'H':
		return Result{Command: CommandSwapLeft}
	case 'L':
		return Result{Command: CommandSwapRight}
	case 'K':
		return Result{Command: CommandSwapUp}
	case 'J':
		return Result{Command: CommandSwapDown}
	case 'c':
		return Result{Command: CommandNewTab}
	case 's':
		return Result{Command: CommandNewSpace}
	case ')':
		return Result{Command: CommandNextSpace}
	case '(':
		return Result{Command: CommandPrevSpace}
	case 'a':
		return Result{Command: CommandToggleAgents}
	case 'g', 'w':
		// w is herdr's workspace picker, which is this: moving through the
		// spaces with a preview. g stays, for hands that learned it here.
		return Result{Command: CommandNavigate}
	case 'm':
		return Result{Command: CommandMenu}
	case ',':
		return Result{Command: CommandRenameTab}
	case '.':
		return Result{Command: CommandRenameSpace}
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return Result{Command: CommandSelectTab, Arg: int(b - '0')}
	case 'n':
		return Result{Command: CommandNextTab}
	case 'p':
		return Result{Command: CommandPrevTab}
	case 'd':
		return Result{Command: CommandDetach}
	case 'r':
		return Result{Command: CommandResizeMode}
	case 'R':
		return Result{Command: CommandRefresh}
	case '?':
		return Result{Command: CommandHelp}
	case in.prefix():
		return Result{Command: CommandLiteralPrefix, Forward: []byte{in.prefix()}}
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
	case 'Z':
		return CommandFocusPrev // shift+tab arrives as ESC [ Z
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
	lines := make([]string, 0, len(Keys)+len(Gestures)+2)
	lines = append(lines, "ctrl+b then:")
	for _, k := range Keys {
		lines = append(lines, "  "+pad(k.Key, 5)+" "+k.Help)
	}
	lines = append(lines, "mouse:")
	for _, g := range Gestures {
		lines = append(lines, "  "+pad(g.Gesture, 9)+" "+g.Help)
	}
	return lines
}

// Gestures are what the mouse does, listed beside the keys because somebody
// looking for how to copy looks in the same place either way.
//
// They are not in Keys: that table maps a key to a command and is checked to
// be exactly that, and a drag is neither.
var Gestures = []struct {
	Gesture string
	Help    string
}{
	{"drag", "select text"},
	{"alt+drag", "select a block"},
	{"right", "menu for what is under it"},
	{"wheel", "scroll back"},
}

func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}
