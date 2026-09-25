//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTheInstallerChecksWhatItDownloads runs site/install.sh — what
// `curl … | sh` runs — against a release served by a stand-in curl: a
// binary that matches the release's SHA256SUMS is installed, and one that
// does not stops the install with nothing put on PATH and no fall back to
// building from source. If it regresses, a download cut short or changed
// on the way is installed and run.
func TestTheInstallerChecksWhatItDownloads(t *testing.T) {
	script, err := filepath.Abs("../../site/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	name := "tend-" + runtime.GOOS + "-" + runtime.GOARCH
	binary := []byte("#!/bin/sh\necho tend v9.9.9\n")
	sum := sha256.Sum256(binary)

	run := func(t *testing.T, sums string) (string, string, error) {
		t.Helper()
		files := t.TempDir()
		write := func(file string, data []byte) {
			if err := os.WriteFile(filepath.Join(files, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("latest", []byte(`{"tag_name": "v9.9.9"}`))
		write(name, binary)
		write("SHA256SUMS", []byte(sums))

		// curl, as the installer calls it, answering from the files: the
		// last part of the URL names the file, and releases/latest is the
		// API's answer.
		bin := t.TempDir()
		curl := `#!/bin/sh
url=""; out=""
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift 2 ;;
	-*) shift ;;
	*) url=$1; shift ;;
	esac
done
case "$url" in
*/releases/latest) f="$FILES/latest" ;;
*) f="$FILES/${url##*/}" ;;
esac
[ -f "$f" ] || exit 22
if [ -n "$out" ]; then cp "$f" "$out"; else cat "$f"; fi
`
		if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0o755); err != nil {
			t.Fatal(err)
		}
		// A git that fails loudly: building from source is not a way round
		// a bad download.
		if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho git was run >&2\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		dest := t.TempDir()
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FILES="+files,
			"TEND_INSTALL_DIR="+dest, "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		return string(out), filepath.Join(dest, "tend"), err
	}

	t.Run("a matching binary is installed", func(t *testing.T) {
		out, installed, err := run(t, hex.EncodeToString(sum[:])+"  "+name+"\n0000  tend-other\n")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(out, "checksum ok") || !strings.Contains(out, "(tend v9.9.9)") {
			t.Errorf("output:\n%s", out)
		}
		if _, err := os.Stat(installed); err != nil {
			t.Errorf("not installed: %v", err)
		}
	})
	t.Run("one that does not match is refused", func(t *testing.T) {
		out, installed, err := run(t, strings.Repeat("0", 64)+"  "+name+"\n")
		if err == nil || !strings.Contains(out, "does not match the release's SHA256SUMS") || strings.Contains(out, "git was run") {
			t.Errorf("err %v, output:\n%s", err, out)
		}
		if _, err := os.Stat(installed); !os.IsNotExist(err) {
			t.Errorf("something was installed: %v", err)
		}
	})
	t.Run("a release with no sum for it is refused", func(t *testing.T) {
		out, installed, err := run(t, hex.EncodeToString(sum[:])+"  tend-other\n")
		if err == nil || !strings.Contains(out, "has no line for "+name) {
			t.Errorf("err %v, output:\n%s", err, out)
		}
		if _, err := os.Stat(installed); !os.IsNotExist(err) {
			t.Errorf("something was installed: %v", err)
		}
	})
}
