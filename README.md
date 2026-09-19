# tend

terminal runtime for coding agents.

tend keeps agent terminals running in a background server, marks every pane as
working, blocked or idle, and exposes that over a socket API so agents can
spawn panes and wait on each other.

Written in Go, single binary, no cgo.

> Status: early but usable. Panes are drawn, keys reach them, and detaching
> leaves everything running. No mouse yet, no reflow on resize, and a client
> does not reconnect by itself if the server restarts.

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

`ctrl+b` is the prefix. `ctrl+b ?` lists the keys; `|` and `-` split, `hjkl`
and the arrows move focus, `x` closes a pane, `d` detaches and leaves
everything running.

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

## license

Apache-2.0. tend is an independent Go implementation of the architecture of
[herdr](https://github.com/herdrdev/herdr); see [NOTICE](NOTICE) for attribution.
