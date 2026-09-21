#!/bin/sh
# install tend — terminal runtime for coding agents
#
#   curl -fsSL https://sousaakira.github.io/tend/install.sh | sh
#
# Prefers a release binary from GitHub. If none matches this machine, builds
# from source with Go. Installs to $TEND_INSTALL_DIR, or $GOBIN, or ~/.local/bin.

set -eu

REPO="sousaakira/tend"
SITE="https://sousaakira.github.io/tend"
RAW="https://raw.githubusercontent.com/${REPO}/master"

say() { printf '%s\n' "$*"; }
die() { printf 'tend install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "need $1 on PATH"; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $arch" ;;
esac
case "$os" in
linux | darwin) ;;
*) die "unsupported OS: $os (linux and macOS only)" ;;
esac

bin_name="tend-${os}-${arch}"

if [ -n "${TEND_INSTALL_DIR:-}" ]; then
	dest=$TEND_INSTALL_DIR
elif [ -n "${GOBIN:-}" ]; then
	dest=$GOBIN
else
	dest=${HOME}/.local/bin
fi
mkdir -p "$dest"
out="${dest}/tend"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT HUP TERM

download() {
	url=$1
	file=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$file"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$file" "$url"
	else
		die "need curl or wget"
	fi
}

# Latest release tag, or empty when there is none.
latest_tag() {
	# Prefer the GitHub API; fall back to the release redirect.
	if command -v curl >/dev/null 2>&1; then
		body=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null) || return 0
		printf '%s' "$body" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1
		return 0
	fi
	# wget without jq: follow the releases/latest redirect.
	loc=$(wget --max-redirect=0 --server-response \
		"https://github.com/${REPO}/releases/latest" 2>&1 |
		sed -n 's/.*Location: .*\/tag\/\([^[:space:]]*\).*/\1/p' | head -n1) || true
	printf '%s' "$loc"
}

install_binary() {
	src=$1
	chmod +x "$src"
	# Atomic replace so a half-written binary never sits on PATH.
	tmp="${out}.new"
	cp "$src" "$tmp"
	mv "$tmp" "$out"
}

from_release() {
	tag=$(latest_tag)
	[ -n "$tag" ] || return 1
	url="https://github.com/${REPO}/releases/download/${tag}/${bin_name}"
	say "downloading ${bin_name} (${tag})"
	if ! download "$url" "${tmpdir}/${bin_name}"; then
		say "no release binary for ${os}/${arch}; building from source"
		return 1
	fi
	install_binary "${tmpdir}/${bin_name}"
	return 0
}

from_source() {
	need git
	go_bin=
	if command -v go >/dev/null 2>&1; then
		go_bin=$(command -v go)
	elif [ -x "${HOME}/.local/go/bin/go" ]; then
		go_bin=${HOME}/.local/go/bin/go
	else
		die "Go is not installed; install Go from https://go.dev/dl/ and re-run, or wait for a release binary"
	fi
	say "cloning ${REPO}"
	git clone --depth 1 "https://github.com/${REPO}.git" "${tmpdir}/tend"
	say "building"
	(
		cd "${tmpdir}/tend"
		CGO_ENABLED=0 "$go_bin" build -ldflags "-s -w" -o "${tmpdir}/tend-bin" ./cmd/tend
	)
	install_binary "${tmpdir}/tend-bin"
}

if ! from_release; then
	from_source
fi

ver=$("$out" version 2>/dev/null || true)
say "installed ${out}${ver:+ ($ver)}"

case ":${PATH}:" in
*":${dest}:"*) ;;
*)
	say "note: ${dest} is not on PATH"
	say "add it with:  export PATH=\"${dest}:\$PATH\""
	;;
esac

say "run:  tend"
