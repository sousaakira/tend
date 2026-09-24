// Package browserext is the browser tend opens: a Chromium-family browser
// in a profile of tend's own, with tend's extension loaded in it and the
// native messaging host the extension talks to tend through registered in
// that profile — so the browser comes up with the extension installed and
// working, and the user's own browser and profile are not touched.
//
// The extension is in extension/, built into the binary, and written out
// each time the browser is prepared, so it is always the one of the tend
// that opened it. Its manifest carries a fixed public key, which fixes its
// ID (ExtensionID) wherever it is written: the host's registration names the
// extension allowed to start it by that ID. The host is `tend browser
// bridge` (docs/BROWSER.md).
package browserext

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed extension
var files embed.FS

// ExtensionID is the extension's, fixed by the key in its manifest.
const ExtensionID = "kafdikfjfbpngnlobakdlnepmciniffa"

// HostName is the native messaging host the extension connects to.
const HostName = "dev.tend.browser"

// Profile is the browser's place on disk, under Root: the extension, the
// script that starts the host, and the browser's own data directory.
type Profile struct {
	Root      string
	Extension string
	Host      string
	UserData  string
}

// DefaultRoot is where the browser's profile lives: tend's data directory.
func DefaultRoot() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "tend", "browser"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "tend", "browser"), nil
}

// Prepare writes the extension, the host's start script — which runs tend,
// the binary at tendBin, as the bridge — and the host's registration in the
// profile's data directory, where a browser started with that directory
// looks for it. It is safe to run each time the browser is opened.
func Prepare(root, tendBin string) (Profile, error) {
	p := Profile{
		Root:      root,
		Extension: filepath.Join(root, "extension"),
		Host:      filepath.Join(root, "tend-browser-host"),
		UserData:  filepath.Join(root, "profile"),
	}
	if err := writeExtension(p.Extension); err != nil {
		return p, err
	}
	// A script, since a host's registration names a program and no
	// arguments: this one is `tend browser bridge`.
	script := "#!/bin/sh\nexec " + shellQuote(tendBin) + " browser bridge \"$@\"\n"
	if err := os.WriteFile(p.Host, []byte(script), 0o755); err != nil {
		return p, err
	}
	manifest, err := json.MarshalIndent(map[string]any{
		"name":            HostName,
		"description":     "tend: the session this browser was opened for",
		"path":            p.Host,
		"type":            "stdio",
		"allowed_origins": []string{"chrome-extension://" + ExtensionID + "/"},
	}, "", "  ")
	if err != nil {
		return p, err
	}
	hosts := filepath.Join(p.UserData, "NativeMessagingHosts")
	if err := os.MkdirAll(hosts, 0o755); err != nil {
		return p, err
	}
	return p, os.WriteFile(filepath.Join(hosts, HostName+".json"), append(manifest, '\n'), 0o644)
}

// writeExtension replaces the extension on disk with the one built in.
func writeExtension(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return fs.WalkDir(files, "extension", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out := filepath.Join(dir, strings.TrimPrefix(path, "extension"))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		raw, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, raw, 0o644)
	})
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// candidates are the browsers tried, in order, when none is set: those
// that load an extension given on their command line. Tried on 2026-09-23,
// headless, with the extension and the bridge: Chromium 153 and Microsoft
// Edge 153 load it; Google Chrome 154 and Brave 153 start without it —
// Google's own Chrome stopped honouring --load-extension in 2025 — so they
// are not candidates, and naming one in [browser] program opens a browser
// the extension is not in. Vivaldi and Chrome for Testing are left in on
// their makers' word, untried.
var candidates = []string{
	"chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable",
	"vivaldi", "google-chrome-for-testing",
}

// ErrNoBrowser is a machine with none of the browsers that can load the
// extension.
var ErrNoBrowser = errors.New("no browser that can load tend's extension (chromium, microsoft-edge)")

// Find is the browser to run: the program given, else the first candidate on
// the PATH.
func Find(program string, lookPath func(string) (string, error)) (string, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if program != "" {
		path, err := lookPath(program)
		if err != nil {
			return "", fmt.Errorf("browser %q: %w", program, err)
		}
		return path, nil
	}
	for _, c := range candidates {
		if path, err := lookPath(c); err == nil {
			return path, nil
		}
	}
	return "", ErrNoBrowser
}

// Args are the browser's arguments: the profile's data directory, the
// extension loaded, no first-run questions, and the page, if any.
func Args(p Profile, url string) []string {
	args := []string{
		"--user-data-dir=" + p.UserData,
		"--load-extension=" + p.Extension,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if url != "" {
		args = append(args, url)
	}
	return args
}
