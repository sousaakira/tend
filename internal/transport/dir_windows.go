//go:build windows

package transport

import (
	"fmt"
	"os"
	"path/filepath"
)

// runtimeDir returns the directory holding this user's session sockets.
//
// Windows has no XDG_RUNTIME_DIR; LOCALAPPDATA is the per-user equivalent and
// is already restricted to its owner. Go supports AF_UNIX on Windows 10 and
// later, so the socket itself works the same way — though running panes does
// not yet, since the pty layer is still a stub there.
// maxSocketPathLen: Windows AF_UNIX has no sun_path limit of its own, so the
// ordinary path limit applies.
const maxSocketPathLen = 260

func runtimeDir() (string, error) {
	if dir := os.Getenv("TEND_RUNTIME_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "tend", "run"), nil
	}
	tmp := os.TempDir()
	if tmp == "" {
		return "", fmt.Errorf("transport: no runtime directory; set TEND_RUNTIME_DIR")
	}
	return filepath.Join(tmp, "tend"), nil
}
