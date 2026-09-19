//go:build unix

package transport

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// runtimeDir returns the directory holding this user's session sockets.
//
// XDG_RUNTIME_DIR is preferred because it is already per-user, already
// private, and cleaned up at logout. The fallback under the temp directory
// includes the uid, so two users on one machine cannot collide or reach each
// other's sockets.
//
// TEND_RUNTIME_DIR overrides both, which is what tests use to avoid touching a
// real session.
// maxSocketPathLen is the size of sun_path: 108 on Linux, 104 on the BSDs and
// macOS. The smaller one is used everywhere, so a path that works on one Unix
// works on all of them rather than failing only on somebody else's machine.
const maxSocketPathLen = 104

func runtimeDir() (string, error) {
	if dir := os.Getenv("TEND_RUNTIME_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "tend"), nil
	}
	tmp := os.TempDir()
	if tmp == "" {
		return "", fmt.Errorf("transport: no runtime directory; set TEND_RUNTIME_DIR")
	}
	return filepath.Join(tmp, "tend-"+strconv.Itoa(os.Getuid())), nil
}
