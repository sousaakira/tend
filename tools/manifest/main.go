// Command manifest writes a release's update manifest, latest.json, from
// `make dist`'s checksums and the release's notes. It is published with the
// release on GitHub, where the stable channel reads it (update.StableManifest).
//
//	go run ./tools/manifest -version v0.4.0 -notes notes.md -dir dist
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sousaakira/tend/internal/update"
)

func main() {
	version := flag.String("version", "", "the release's tag, v0.4.0")
	notes := flag.String("notes", "", "a file holding the release's notes, in markdown")
	dir := flag.String("dir", "dist", "where the binaries and SHA256SUMS are, and where latest.json goes")
	repo := flag.String("repo", "sousaakira/tend", "the GitHub repository the release is in")
	flag.Parse()
	if err := run(*version, *notes, *dir, *repo); err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
}

func run(version, notesPath, dir, repo string) error {
	if _, ok := update.ParseVersion(version); !ok {
		return fmt.Errorf("-version %q is not a release version (v1.2.3)", version)
	}
	if notesPath == "" {
		return fmt.Errorf("-notes is required: a release is announced by its notes")
	}
	notes, err := os.ReadFile(notesPath)
	if err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	base := "https://github.com/" + repo + "/releases/download/" + version
	raw, err := update.NewManifest(version, string(notes), base, string(sums))
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "latest.json")
	if err := os.WriteFile(out, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
