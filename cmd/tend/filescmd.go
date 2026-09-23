package main

import (
	"errors"
	"flag"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/config"
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
	m.FollowPanes(opener)
	// Leniently: this panel may be older than the tend that wrote the file,
	// and a setting it does not know is no reason to lose the ones it does.
	// A file it cannot read at all is said, not silently replaced by the
	// defaults — which is how icons set on the settings screen went missing.
	var settingsErr error
	settings := func() explorer.Settings {
		cfg, err := config.LoadLenient()
		settingsErr = err
		dock := "right"
		if cfg.FilesOnLeft() {
			dock = "left"
		}
		return explorer.Settings{
			Icons: cfg.Files.Icons, Hidden: cfg.Files.Hidden, Follow: cfg.FilesFollow() && !*still,
			Dock: dock, Width: cfg.FilesWidth(),
		}
	}
	m.Configure(settings())
	// The gear's settings write the same [files] the settings screen does.
	m.SetSettingsWriter(func(key, value string) error { return config.Set("files", key, value) })
	if settingsErr != nil {
		m.Warn("settings: " + settingsErr.Error())
	}
	// Read again when the file changes, so a choice made on the settings
	// screen shows in a panel already open.
	path, _ := config.Path()
	var seen time.Time
	if info, err := os.Stat(path); err == nil {
		seen = info.ModTime()
	}
	m.WatchSettings(func() (explorer.Settings, bool) {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Equal(seen) {
			return explorer.Settings{}, false
		}
		seen = info.ModTime()
		s := settings()
		if settingsErr != nil {
			m.Warn("settings: " + settingsErr.Error())
		}
		return s, true
	})
	return explorer.Run(m, os.Stdin, os.Stdout)
}
