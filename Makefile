GO ?= go

# Packages under active development. `dev` narrows to these so the loop stays
# fast as the tree grows; widen it when a package graduates.
DEV_PKGS ?= ./internal/...

.PHONY: dev watch test test-race check fmt vet bench build clean

## dev: fastest feedback loop — cached, stops at the first failure, skips vet.
# `go test` runs vet by default; -vet=off is most of the speedup here. Use
# `make check` before committing, which puts vet back.
dev:
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
test:
	$(GO) test ./...

## test-race: full run under the race detector. Slow; for concurrent code.
test-race:
	$(GO) test -race ./...

## check: what must pass before committing.
check: fmt vet test

fmt:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

## bench: hot-path benchmarks. internal/vt must stay at 0 allocs/op.
bench:
	$(GO) test -bench=. -benchmem -run=XXX ./internal/vt/

build:
	$(GO) build -o bin/tend ./cmd/tend

clean:
	rm -rf bin
	$(GO) clean -testcache
