# tend

terminal runtime for coding agents.

tend keeps agent terminals running in a background server, marks every pane as
working, blocked or idle, and exposes that over a socket API so agents can
spawn panes and wait on each other.

Written in Go, single binary, no cgo.

> Status: usable. Panes are drawn, keys and the mouse reach them, text reflows
> when you resize, scrollback is readable, the client reconnects by itself, and
> detaching leaves everything running.

## try it

```bash
make build

bin/tend agents -v              # the agents tend can detect
bin/tend watch -- claude        # run an agent and report its state live
bin/tend watch -capture out.raw -- codex
bin/tend detect -agent claude out.raw    # replay a capture offline
bin/tend explain -agent claude out.raw   # ...and see how every rule voted
bin/tend screen out.raw                  # ...and what the terminal core made of it

bin/tend session -- claude -- codex      # several panes in one process
```

against a running server:

```bash
bin/tend
```

That is the whole thing. It opens a session, starting a server for it if there
is not one already, and draws it.

```
┌ 1 claude ●──────────────────────────┐┌ 2 codex ▲───────────────────────────┐
│⠋ Pondering the refactor…            ││ Do you want to proceed?             │
│                                     ││  ❯ 1. Yes                           │
│                                     ││    2. No                            │
└─────────────────────────────────────┘└─────────────────────────────────────┘
 work · main · agents   1:claude ●  2:codex ▲
```

`ctrl+b` is the prefix and `ctrl+b ?` lists every key.

| | |
|---|---|
| `\|` `-` | split beside, below |
| `hjkl` arrows | move focus |
| `HJKL` | resize |
| `z` | zoom a pane to the window |
| `[` | scroll back through its history |
| `x` | close a pane |
| `c` `n` `p` `1-9` | tabs: new, next, previous, by number |
| `s` `(` `)` | spaces: new, previous, next |
| `a` | show every agent, grouped |
| `g` | find any space, tab or pane: search, filter by state, jump |
| `w` | walk the agent list from the keyboard |
| `,` `.` | rename this tab, this space |
| `f` | the files panel: the project, git, a diff, a search |
| `d` | detach, leaving everything running |

A **space** holds tabs, a tab holds panes. Tabs belong to their space, so
`ctrl+b n` stays where you are and `ctrl+b )` is how you leave.

`ctrl+b a` opens the agent list down the left: every agent in the session,
grouped by space and tab, with what it is doing. `ctrl+b w` walks it and Enter
jumps — which is the point, since the agent that stopped is rarely the one you
are looking at.

`ctrl+b f` docks the files panel on the left of the tab: the project as a
tree with git's letter beside each changed file, the changes with their diffs
(stage with `s`, commit with `c`), `/` to find any file by name, and
`ctrl+f` to search the files' text (case, whole word, regex, include and
exclude globs) and open the editor on the line found. It follows the pane
beside it: when that pane's program moves to another project, the panel goes
there too (`tend files -still` stays put). The line under its header is the
branch: click it (or `B`) to switch branch, or to make a local one from a
remote; `⟳` (or `P`) syncs — pull when behind, fast-forward only, push when
ahead, publish a branch with no upstream to origin — in the background, and
never asks for a password on the panel. In the changes view, `A` drafts the
commit message with the local `claude` CLI (haiku), or from the file names
without it, and puts it in the commit box to read before Enter. Enter
opens a file in `$EDITOR` in a tab of its own; space, or a click, shows it
read-only in a preview pane beside the main one, with syntax colour, kept for
the next file (`tend view` is that preview on its own). In the tree, `s` stages a file or
a whole folder, and `m` (or a right-click) is the menu: new file or folder,
rename, delete (type yes), copy the path, open with the system's app, reveal
in the file manager, change the folder shown, a file's history. `L` is the
history: commits, stashes (apply, pop, drop) and tags, each shown with its
diff. It runs on the machine the
session is on, so over `--remote` it shows the server's project. `ctrl+b f`
again goes to it, and once more puts it away.

`ctrl+b g` is the navigator: every space, tab and pane in one list over the
screen, each pane with its agent's state. `/` searches by name or directory,
`b` `w` `i` `d` keep only blocked, working, idle or done agents (`a` shows all
again), space opens a space, Enter goes there.

The mouse works throughout. Click a tab to switch to it, `+` to make one,
click a pane to focus it, drag a border to resize, scroll to look back. In the
agent list, clicking a row goes there — a pane, a tab, or a whole space. A pane
running something that wants the mouse itself, an editor say, gets the clicks
instead.

## settings

Optional, at `~/.config/tend/config.toml`. `tend config -init` writes a
commented copy of the defaults; `tend config` says where it is and whether it
loads.

```toml
[keys]
prefix = "ctrl+a"

[pane]
shell = ["/bin/zsh"]
scrollback = 20000

[ui]
mouse = true

[ui.theme]
border_focused = "blue"
blocked = "#ff5f5f"
```

A misspelled setting is an error rather than being ignored, since a setting
quietly dropped is one you believe is in effect.

The rest of the commands work against the same server, from anywhere:

```bash
bin/tend new -- claude          # open a pane; prints its id
bin/tend new -split 1 -dir rows -- codex
bin/tend ls                     # what is running
bin/tend follow                 # watch state changes, for diagnosis
bin/tend kill 2                 # close one pane
bin/tend kill -server           # stop the session
```

`attach` and `new` start a server when there is none; everything else reports
a missing session instead, since creating one on the way to listing it would
report an empty session rather than the absence of one. `tend serve` runs a
server in the foreground, which is for watching it rather than for using it.

The panes belong to the server, not to the command that opened them: every
line above is a separate process, and the agents keep working between them.
`-s NAME` runs more than one session side by side.

`watch` runs one command on a pseudo-terminal, mirrors its output, and prints
state changes to stderr:

```
[tend] watching claude as "claude" on a 120x40 terminal
[tend] working   osc_title_working
[tend] blocked   bash_permission_prompt
[tend] idle      live_prompt_box
```

`-capture` writes the raw bytes, which `detect`, `explain` and `screen` replay
offline. That loop is how a detection rule gets debugged: capture the state
once, then iterate against the file instead of against a live agent.

`session` runs each command as a pane in one server and reports their state as
it changes. It does not draw the panes — that is the client's job, and the
client does not exist yet — but it is the multiplexer underneath:

```
PANE  COMMAND  AGENT   STATE    TITLE
1     claude   claude  working  my-project
2     codex    codex   blocked  ~/proj
3     htop     -       -        -
```

## install

```bash
curl -fsSL https://sousaakira.github.io/tend/install.sh | sh
```

Or from a checkout:

```bash
make install     # into GOBIN, or ~/.local/bin
make dist        # cross-compiled binaries for linux and macOS
```

## development

```bash
make dev      # fast loop: cached, fail-fast, no vet (~50ms warm)
make watch    # re-run `make dev` on every .go change
make check    # gofmt + vet + full tests — run before committing
make bench    # parser hot-path benchmarks; internal/vt must stay 0 allocs/op
```

`make dev` narrows to `./internal/...` and turns off the vet pass that
`go test` runs by default, which is where most of the time goes. `make check`
puts both back.

Requires Go 1.27+.

## architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## working on tend

tend is a port of herdr to Go, and the porting is ongoing.

- [AGENTS.md](AGENTS.md) — how to work here: the rules, the gate, the traps.
  Written for AI agents; it is the right first read for anyone.
- [docs/PORTING.md](docs/PORTING.md) — what has been ported, what has not, and
  where each missing piece lives in herdr's source. The work queue.

## license

Apache-2.0. tend is an independent Go implementation of the architecture of
[herdr](https://github.com/herdrdev/herdr); see [NOTICE](NOTICE) for attribution.
