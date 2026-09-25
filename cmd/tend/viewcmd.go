package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/explorer"
	"github.com/auth-com-br/tend/internal/server"
)

// runView is the files panel's preview: one file, read-only, with line
// numbers and syntax colour, read again when it changes. The panel opens it
// in a pane beside the main one and points it at the next file; run by hand
// it is a pager that knows code.
func runView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	line := fs.Int("line", 0, "line to show and mark, from 1")
	editor := fs.String("editor", "", "command o edits with (default: $VISUAL, $EDITOR, vi)")
	fs.Usage = func() {
		fs.Output().Write([]byte("usage: tend view [-line n] <file>\n\n" +
			"Shows a file read-only with line numbers and syntax colour;\n" +
			"the files panel's preview. q closes, o opens it in the editor,\n" +
			"w wraps long lines.\n\n"))
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("view draws on a terminal, and stdin is not one")
	}
	path, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if *editor == "" {
		*editor = server.Editor()
	}
	opener := &explorer.SessionOpener{
		Socket: os.Getenv(api.EnvSocketPath),
		Pane:   os.Getenv(api.EnvPaneID),
		Editor: *editor,
	}
	return explorer.RunPreview(explorer.NewPreview(path, *line, opener), os.Stdin, os.Stdout)
}
