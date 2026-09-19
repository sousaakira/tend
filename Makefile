# Go is not always on PATH — the toolchain may live in ~/.local/go. Resolve it
# here so `make` works regardless of how the shell is configured. Override with
# `make GO=/path/to/go`.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)


# Packages under active development. `dev` narrows to these so the loop stays
# fast as the tree grows; widen it when a package graduates.
DEV_PKGS ?= ./internal/...

# `make` with no arguments runs the fast loop, not the first target in the file.
.DEFAULT_GOAL := dev

.PHONY: toolchain dev watch test test-race check fmt vet bench build clean

## toolchain: fail with a usable message instead of "go: No such file or directory".
toolchain:
	@command -v $(GO) >/dev/null 2>&1 || test -x $(GO) || { \
		echo "go not found: looked on PATH and in ~/.local/go/bin"; \
		echo "install Go, or run: make GO=/path/to/go"; \
		exit 1; }

## dev: fastest feedback loop — cached, stops at the first failure, skips vet.
# `go test` runs vet by default; -vet=off is most of the speedup here. Use
# `make check` before committing, which puts vet back.
dev: toolchain
	@$(GO) test -vet=off -failfast -short $(DEV_PKGS)

## watch: re-run `make dev` whenever a .go file changes.
# Polls instead of depending on entr/fswatch/watchexec, none of which are
# assumed to be installed. The signature covers both the file list and the
# file contents, so adds, removes and edits all trigger while a bare touch
# does not. cksum keeps it portable across Linux and macOS.
watch:
	@echo "watching .go files — ctrl-c to stop"
	@last=""; \
	while true; do \
		sig=$$( { find . -name '*.go' -type f | sort; \
		          find . -name '*.go' -type f -exec cat {} + ; } | cksum ); \
		if [ "$$sig" != "$$last" ]; then \
			last="$$sig"; \
			printf '\033[2J\033[H'; \
			date +'--- %H:%M:%S ---'; \
			$(MAKE) --no-print-directory dev || true; \
		fi; \
		sleep 1; \
	done

## test: full test run.
test: toolchain
	$(GO) test ./...

## test-race: full run under the race detector. Slow; for concurrent code.
test-race: toolchain
	$(GO) test -race ./...

## check: what must pass before committing.
check: toolchain fmt vet test

fmt:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet: toolchain
	$(GO) vet ./...

## bench: hot-path benchmarks. internal/vt must stay at 0 allocs/op.
bench: toolchain
	$(GO) test -bench=. -benchmem -run=XXX ./internal/vt/

build: toolchain
	$(GO) build -o bin/tend ./cmd/tend

clean:
	rm -rf bin
	$(GO) clean -testcache
