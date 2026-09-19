# tend

terminal runtime for coding agents.

tend keeps agent terminals running in a background server, marks every pane as
working, blocked or idle, and exposes that over a socket API so agents can
spawn panes and wait on each other.

Written in Go, single binary, no cgo.

> Status: early. The terminal core — parser, grid, scrollback and screen —
> works and is tested. The PTY layer, agent detection, server, TUI and CLI are
> still to come. Not yet usable.

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
