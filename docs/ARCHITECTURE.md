# tend — architecture

tend is a terminal runtime for coding agents: it keeps agent terminals alive in
a background server, tracks whether each agent is working, blocked or idle, and
exposes that over a socket API so agents can drive each other.

It is an independent Go implementation of the architecture proven by
[herdr](https://github.com/herdrdev/herdr) (Apache-2.0). See `NOTICE`.

## Principles

These are load-bearing. A change that breaks one needs a reason written down.

1. **State is separate from runtime.** Session state is plain data, testable
   without PTYs or goroutines. A pane's state is not its process.
2. **Render is pure.** Layout computation mutates; drawing does not. The
   renderer takes a read-only snapshot and produces cells. No state changes
   during a draw.
3. **Detection is decoupled.** The detector reads a screen snapshot and nothing
   else. It never touches the parser, the PTY, or viewport state.
4. **Platform code is isolated.** OS behaviour lives behind build tags in
   `internal/pty` and `internal/platform`. Core packages contain no
   `runtime.GOOS` checks.
5. **No god packages.** When a package starts doing two jobs, split it before
   it grows a third.

## Multiplicative cost

Work reachable from parsing, rendering, layout and client fan-out is
multiplicative: per byte or event × panes × attached clients. Before adding work
to those paths, establish its frequency and cardinality.

Concretely, for `internal/vt` and the render loop:

- No allocation per byte, per cell or per frame. `Parser.Parse` is covered by
  benchmarks asserting `0 allocs/op`; keep them at zero.
- Hidden panes still parse output, but must not trigger presentation work.
- Hold locks over terminal state for as short a window as possible.

## Packages

| Package | Role |
|---|---|
| `internal/vt` | Terminal emulator core: escape-sequence parser, grid, screen state |
| `internal/pty` | PTY creation and process lifecycle, per platform |
| `internal/detect` | Manifest-driven agent state detection |
| `internal/session` | Pure state: workspaces, tabs, panes, layout |
| `internal/server` | Background daemon owning panes and terminals |
| `internal/proto` | Client/server wire messages |
| `internal/client` | TUI client |
| `internal/ui` | Pure rendering |
| `cmd/tend` | CLI entry point |

## Terminal core

`internal/vt` is written from scratch rather than binding an existing C
emulator. The trade is deliberate: no cgo, so cross-compilation stays trivial,
at the cost of owning correctness.

The mitigation is scope plus evidence. tend targets what a coding-agent
multiplexer actually needs, not xterm compatibility: CSI/OSC/DCS parsing, an
SGR-styled grid with scrollback, alt screen, mouse modes, and enough reflow to
resize panes without losing history. Everything outside that is explicitly out
of scope until something needs it.

`Parser` follows the [Paul Williams DEC ANSI state
machine](https://vt100.net/emu/dec_ansi_parser) with three extensions —
incremental UTF-8 decoding, colon-separated CSI sub-parameters, and APC capture
— and two deliberate deviations from xterm, both documented in the package
comment: 8-bit C1 controls are never honoured, and over-long string payloads are
dropped rather than truncated.

The property that matters most is chunk independence: PTY reads land on
arbitrary byte boundaries, so parsing a stream one byte at a time must produce
exactly the events that parsing it in one write produces.
`TestParseIsChunkIndependent` pins that.

### What the core implements today

`Parser` (escape-sequence machine), `Grid` (cells, rows, scrollback ring) and
`Screen` (the Handler that turns events into screen state): printing with
wrap, wide characters and combining marks; cursor movement and addressing;
erase, insert and delete for both cells and lines; scroll regions; SGR
including 256-colour and direct colour in both the semicolon and colon forms;
the alternate screen; mouse and paste modes; titles; and cursor reports.

Deliberate gaps, each a decision rather than an oversight:

- **No reflow.** Narrowing a pane truncates instead of rewrapping. `Row`
  already tracks the wrapped flag reflow needs; the algorithm lands when the
  renderer does.
- **No character sets.** tend is UTF-8 only. `ESC ( B` and friends are
  consumed and ignored.
- **No 8-bit C1 controls**, for the UTF-8 reason in the package comment.
- **DCS and APC payloads are captured and discarded.** kitty graphics will
  read them later without the parser changing.

Two invariants hold the core together, both pinned by tests:

- **Chunk independence.** Feeding a session one byte at a time must land on
  exactly the screen a single write produces. PTY reads split anywhere.
- **No allocation in steady state.** Parsing and screen writes are
  `0 allocs/op`, and a full scrollback ring recycles rows rather than growing.

## Detection

Agent state detection is declarative. A manifest per agent describes ordered
rules over regions of the bottom of the screen buffer, with AND/OR gates and
negative guards.

Manifests are derived from herdr's, which is the single most valuable thing
tend inherits: they encode calibration against 22 real agent UIs. They port
almost unchanged because Rust's `regex` and Go's `regexp` are both RE2 — no
lookaround, no backreferences, same `\x{...}` and `(?m)` syntax.

Two rules carried over from that lineage, both learned the hard way:

- Detect from the **bottom buffer**, never the user-visible viewport. Users
  scroll; agent state does not move.
- Gate on controls that are invariant for a state. Never match incidental text
  that happens to appear on screen.

## Non-goals for the core milestone

Plugins, SSH/multi-machine, kitty graphics, worktree management and session
handoff are all deferred. The architecture should not make them hard to add,
but nothing ships for them until the core is solid.
