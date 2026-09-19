# tend

terminal runtime for coding agents.

tend keeps agent terminals running in a background server, marks every pane as
working, blocked or idle, and exposes that over a socket API so agents can
spawn panes and wait on each other.

Written in Go, single binary, no cgo.

> Status: early. The terminal core, agent detection for 22 agents, and the pty
> layer work and are tested, and the CLI below runs. The server and the TUI
> client — the parts that make it a multiplexer — are still to come.

## try it

```bash
make build

bin/tend agents -v              # the agents tend can detect
bin/tend watch -- claude        # run an agent and report its state live
bin/tend watch -capture out.raw -- codex
bin/tend detect -agent claude out.raw    # replay a capture offline
bin/tend explain -agent claude out.raw   # ...and see how every rule voted
bin/tend screen out.raw                  # ...and what the terminal core made of it
```

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
