package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sousaakira/tend/internal/config"
)

// runConfig shows where the settings live, or writes a starting point.
//
// The example is written commented out and set to the defaults, so that
// opening it answers "what can I change?" without a manual, and deleting a
// line is always safe.
func runConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	initialise := fs.Bool("init", false, "write a commented example file if there is none")
	force := fs.Bool("force", false, "overwrite an existing file")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend config [-init [-force]]\n\n"+
				"prints the settings file's path and whether it loads.\n"+
				"with -init, writes a commented example to it.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, err := config.Path()
	if err != nil {
		return err
	}

	if *initialise {
		if _, err := os.Stat(path); err == nil && !*force {
			return fmt.Errorf("%s already exists; pass -force to overwrite it", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(config.Example), 0o600); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
		return nil
	}

	fmt.Printf("path  %s\n", path)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("state (none; using defaults — \"tend config -init\" writes one)")
		return nil
	}

	// Loading is reported rather than assumed: a file that does not load is
	// the thing someone running this command most needs to know.
	if _, err := config.LoadFile(path); err != nil {
		fmt.Printf("state %v\n", err)
		return err
	}
	fmt.Println("state loads cleanly")
	return nil
}
