package main

import (
	"errors"
	"flag"
	"os"

	"golang.org/x/term"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/explorer"
	"github.com/sousaakira/tend/internal/server"
)

// runFiles is the file explorer. prefix+f docks it on the left of a tab in
// a pane of its own, running on the session's machine; run by hand it is
// the same program, and opens files in the session it finds in its
// environment.
func runFiles(args []string) error {
	fs := flag.NewFlagSet("files", flag.ExitOnError)
	editor := fs.String("editor", "", "command to edit files with (default: $VISUAL, $EDITOR, vi)")
	still := fs.Bool("still", false, "stay in the directory given rather than follow the pane beside the panel")
	fs.Usage = func() {
		fs.Output().Write([]byte("usage: tend files [-editor cmd] [directory]\n\n" +
			"A tree of the project with what git says about each file, the\n" +
			"changes with their diffs, and a search for any file by name.\n" +
			"prefix+f opens it docked on the left of the current tab.\n\n"))
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("files draws on a terminal, and stdin is not one")
	}
	dir := fs.Arg(0)
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}
	if *editor == "" {
		*editor = server.Editor()
	}
	opener := &explorer.SessionOpener{
		Socket: os.Getenv(api.EnvSocketPath),
		Pane:   os.Getenv(api.EnvPaneID),
		Editor: *editor,
	}
	m := explorer.New(dir, opener)
	if !*still {
		m.FollowPanes(opener)
	}
	return explorer.Run(m, os.Stdin, os.Stdout)
}
