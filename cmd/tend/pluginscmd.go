package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/plugin"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/transport"

	"golang.org/x/term"
)

// `tend plugin …`. Linking and listing work without a running server, because
// the registry is a file and a plugin should be installable before the session
// that will use it exists. Invoking one needs a server, since an action acts
// on a session.

// pluginRegistryPath is where the list of installed plugins lives: beside the
// settings file, as herdr keeps its own beside its config.
func pluginRegistryPath() (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "plugins.json"), nil
}

// pluginDirs are where each plugin keeps its own settings and state.
func pluginDirs() (configDir, stateDir string) {
	if path, err := config.Path(); err == nil {
		configDir = filepath.Join(filepath.Dir(path), "plugins")
	}
	if path, err := transport.StatePath("plugins"); err == nil {
		stateDir = strings.TrimSuffix(path, ".json")
	}
	return configDir, stateDir
}

// openRegistry reads the registry from disk.
func openRegistry() (*plugin.Registry, error) {
	path, err := pluginRegistryPath()
	if err != nil {
		return nil, err
	}
	return plugin.OpenRegistry(path)
}

// pluginHost builds what a server needs to run plugins. A registry that cannot
// be read is reported and then left behind: a damaged plugin list must not
// stop a session from starting.
func pluginHost(env []string) *server.Plugins {
	registry, err := openRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", tag(), err)
	}
	if registry == nil {
		return nil
	}
	configDir, stateDir := pluginDirs()
	return &server.Plugins{
		Registry: registry, Env: env, ConfigDir: configDir, StateDir: stateDir,
	}
}

// pastTense is what each command says it did, written out rather than derived:
// "disable" + "d" is not a word.
var pastTense = map[string]string{
	"unlink": "unlinked", "enable": "enabled", "disable": "disabled", "reload": "reloaded",
}

func runPlugin(args []string) error {
	usage := func(w io.Writer) {
		fmt.Fprint(w,
			"usage: tend plugin <command>\n\n"+
				"  list                     installed plugins\n"+
				"  install <owner/repo[/dir]> [-ref REF] [-yes]\n"+
				"                           install a plugin from GitHub\n"+
				"  uninstall <id|owner/repo[/dir]>\n"+
				"                           remove it, and its files if tend installed them\n"+
				"  link <directory>         install the plugin in that directory\n"+
				"  build <id>               run its build steps again\n"+
				"  unlink <id>              forget it (its files are left alone)\n"+
				"  enable <id> | disable <id>\n"+
				"  reload <id>              re-read its manifest after editing it\n"+
				"  actions                  what the installed plugins offer\n"+
				"  log [id]                 the last runs of plugin commands in the session\n"+
				"  run <action-id>          invoke an action in the running session\n"+
				"  open <pane-id>           open a pane a plugin offers\n\n"+
				"a plugin is a directory with a "+plugin.ManifestName+" in it. linking records\n"+
				"where it is; the directory stays yours.\n\n")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no command given")
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "list":
		registry, err := openRegistry()
		if err != nil {
			return err
		}
		installed := registry.List()
		if len(installed) == 0 {
			path, _ := pluginRegistryPath()
			fmt.Fprintf(os.Stderr, "%s no plugins installed (%s)\n", tag(), path)
			return nil
		}
		t := newTable("PLUGIN", "VERSION", "STATE", "ROOT")
		for _, p := range installed {
			state := "enabled"
			if !p.Enabled {
				state = "disabled"
			}
			t.row(p.ID, p.Version, state, p.Root)
		}
		if err := t.flush(); err != nil {
			return err
		}
		for _, p := range installed {
			for _, w := range p.Warnings {
				fmt.Fprintf(os.Stderr, "%s %s: %s\n", tag(), p.ID, w)
			}
		}
		return nil

	case "install":
		return installFromGithub(rest)

	case "log":
		fs := flag.NewFlagSet("plugin log", flag.ExitOnError)
		name := sessionFlag(fs)
		limit := fs.Int("limit", 50, "how many runs to show")
		if err := fs.Parse(hoistFlags(rest, map[string]bool{"s": true, "ssh": true, "limit": true})); err != nil {
			return err
		}
		params := map[string]any{"limit": *limit}
		if fs.NArg() > 0 {
			params["plugin_id"] = fs.Arg(0)
		}
		result, err := apiCall(*name, api.MethodPluginLogList, params, false)
		if err != nil {
			return err
		}
		logs, _ := result["logs"].([]any)
		t := newTable("PLUGIN", "WHAT", "STATUS", "EXIT", "OUTPUT")
		for _, raw := range logs {
			e, _ := raw.(map[string]any)
			what := text(e["action_id"])
			if what == "" {
				what = text(e["event"])
			}
			exit := ""
			if code, ok := e["exit_code"].(float64); ok {
				exit = strconv.Itoa(int(code))
			}
			out := strings.ReplaceAll(text(e["stdout"])+" "+text(e["error"]), "\n", " ")
			if len(out) > 60 {
				out = out[:60] + "…"
			}
			t.row(text(e["plugin_id"]), what, text(e["status"]), exit, strings.TrimSpace(out))
		}
		return t.flush()

	case "uninstall":
		return uninstallPlugin(rest)

	case "link":
		fs := flag.NewFlagSet("plugin link", flag.ExitOnError)
		skipBuild := fs.Bool("no-build", false, "record it without running its build steps")
		rest = hoistFlags(rest, map[string]bool{"no-build": false})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("usage: tend plugin link <directory>")
		}
		registry, err := openRegistry()
		if err != nil {
			return err
		}
		installed, err := registry.Link(fs.Arg(0))
		if err != nil {
			return err
		}
		if !*skipBuild && len(installed.Build) > 0 {
			// Built before it is called useful: a plugin whose binary does
			// not exist yet is one whose every action fails later, somewhere
			// else. A failure unlinks it rather than leaving a plugin that
			// cannot run.
			if err := plugin.Build(installed, nil, os.Stderr); err != nil {
				_ = registry.Unlink(installed.ID)
				return err
			}
			// Re-read: a build that writes the manifest — generating its own
			// actions, say — has changed what was recorded a moment ago.
			if updated, err := registry.Reload(installed.ID); err == nil {
				installed = updated
			}
		}
		fmt.Fprintf(os.Stderr, "%s linked %s %s from %s\n", tag(), installed.ID, installed.Version, installed.Root)
		for _, w := range installed.Warnings {
			fmt.Fprintf(os.Stderr, "%s %s\n", tag(), w)
		}
		// A server that is already running holds its own copy of the registry
		// and will not see this until it re-reads it.
		fmt.Fprintf(os.Stderr, "%s restart the session's server for a running session to pick it up\n", tag())
		return nil

	case "build":
		if len(rest) == 0 {
			return errors.New("usage: tend plugin build <id>")
		}
		registry, err := openRegistry()
		if err != nil {
			return err
		}
		installed, ok := registry.Get(rest[0])
		if !ok {
			return fmt.Errorf("no plugin called %q is installed", rest[0])
		}
		if len(installed.Build) == 0 {
			fmt.Fprintf(os.Stderr, "%s %s has no build steps\n", tag(), installed.ID)
			return nil
		}
		return plugin.Build(installed, nil, os.Stderr)

	case "unlink", "enable", "disable", "reload":
		if len(rest) == 0 {
			return fmt.Errorf("usage: tend plugin %s <id>", sub)
		}
		registry, err := openRegistry()
		if err != nil {
			return err
		}
		id := rest[0]
		switch sub {
		case "unlink":
			err = registry.Unlink(id)
		case "enable":
			err = registry.SetEnabled(id, true)
		case "disable":
			err = registry.SetEnabled(id, false)
		case "reload":
			_, err = registry.Reload(id)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s %s: %s\n", tag(), pastTense[sub], id)
		return nil

	case "actions":
		fs := flag.NewFlagSet("plugin actions", flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		result, err := apiCall(*name, api.MethodPluginActionList, nil, false)
		if err != nil {
			return err
		}
		actions, _ := result["actions"].([]any)
		if len(actions) == 0 {
			fmt.Fprintf(os.Stderr, "%s no plugin offers an action\n", tag())
			return nil
		}
		t := newTable("ACTION", "PLUGIN", "TITLE")
		for _, raw := range actions {
			a, _ := raw.(map[string]any)
			t.row(text(a["id"]), text(a["plugin_id"]), text(a["title"]))
		}
		return t.flush()

	case "run":
		fs := flag.NewFlagSet("plugin run", flag.ExitOnError)
		name := sessionFlag(fs)
		pane := fs.String("pane", "", "the pane the action is about")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("usage: tend plugin run <action-id>")
		}
		params := map[string]any{"action_id": fs.Arg(0)}
		if *pane != "" {
			params["pane_id"] = *pane
		}
		result, err := apiCall(*name, api.MethodPluginActionInvoke, params, true)
		if err != nil {
			return err
		}
		if out := text(result["output"]); out != "" {
			fmt.Println(out)
		}
		if msg := text(result["error"]); msg != "" {
			return errors.New(msg)
		}
		if code, ok := result["exit_code"].(float64); ok && code != 0 {
			return fmt.Errorf("the action exited with %d", int(code))
		}
		return nil

	case "open":
		fs := flag.NewFlagSet("plugin open", flag.ExitOnError)
		name := sessionFlag(fs)
		beside := fs.String("beside", "", "the pane to split")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("usage: tend plugin open <pane-id>")
		}
		params := map[string]any{"pane": fs.Arg(0)}
		if *beside != "" {
			params["pane_id"] = *beside
		}
		result, err := apiCall(*name, api.MethodPluginPaneOpen, params, false)
		if err != nil {
			return err
		}
		return printJSON(result)
	}

	usage(os.Stderr)
	return fmt.Errorf("unknown command %q", sub)
}

// managedCheckouts is where plugins installed from GitHub are kept: tend's
// own directory, not the user's, since tend removes them again.
func managedCheckouts() (string, error) {
	path, err := transport.StatePath("plugin-checkouts")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(path, ".json"), nil
}

// installFromGithub is herdr's `plugin install owner/repo[/subdir]`: clone,
// show what it is, agree, build, then move it where tend keeps it and link
// it. Nothing is left behind by a step that fails.
func installFromGithub(args []string) error {
	fs := flag.NewFlagSet("plugin install", flag.ExitOnError)
	ref := fs.String("ref", "", "a branch, tag or commit to install instead of the default branch")
	yes := fs.Bool("yes", false, "install without asking")
	fs.BoolVar(yes, "y", false, "install without asking")
	if err := fs.Parse(hoistFlags(args, map[string]bool{"ref": true, "yes": false, "y": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: tend plugin install <owner>/<repo>[/subdir...] [-ref REF] [-yes]")
	}
	src, err := plugin.ParseGithubSource(fs.Arg(0))
	if err != nil {
		return err
	}
	if !*yes && !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("installing from GitHub asks first; pass -yes when nobody is there to answer")
	}
	root, err := managedCheckouts()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(root, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	checkout := filepath.Join(temp, "checkout")
	fmt.Fprintf(os.Stderr, "%s cloning %s\n", tag(), src.RemoteURL())
	commit, err := plugin.Checkout(src, *ref, checkout)
	if err != nil {
		return err
	}
	manifestRoot := filepath.Join(checkout, filepath.FromSlash(src.Subdir))
	manifest, warnings, err := plugin.Load(manifestRoot)
	if err != nil {
		return err
	}
	registry, err := openRegistry()
	if err != nil {
		return err
	}
	if existing, ok := registry.Get(manifest.ID); ok && (existing.Source == nil || existing.Source.Kind != "github") {
		return fmt.Errorf("plugin %s is linked from %s; unlink it before installing it from GitHub", manifest.ID, existing.Root)
	}

	fmt.Fprintf(os.Stderr, "%s %s %s from %s at %s\n", tag(), manifest.ID, manifest.Version, src, shortCommit(commit))
	if manifest.Description != "" {
		fmt.Fprintf(os.Stderr, "%s %s\n", tag(), manifest.Description)
	}
	for _, step := range manifest.Build {
		fmt.Fprintf(os.Stderr, "%s builds with: %s\n", tag(), strings.Join(step.Command, " "))
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "%s %s\n", tag(), w)
	}
	if !*yes && !confirm("install this plugin?") {
		fmt.Fprintf(os.Stderr, "%s not installed\n", tag())
		return nil
	}

	// Built where it was cloned, so a failed build leaves nothing installed.
	preview := plugin.Installed{Manifest: manifest, Root: manifestRoot}
	if len(manifest.Build) > 0 {
		if err := plugin.Build(preview, nil, os.Stderr); err != nil {
			return err
		}
	}

	final := filepath.Join(root, manifest.ID)
	backup := filepath.Join(temp, "previous")
	hadPrevious := false
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, backup); err != nil {
			return err
		}
		hadPrevious = true
	}
	if err := os.Rename(checkout, final); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, final)
		}
		return err
	}
	installed, err := registry.Link(filepath.Join(final, filepath.FromSlash(src.Subdir)))
	if err == nil {
		err = registry.SetSource(installed.ID, plugin.Source{
			Kind: "github", Owner: src.Owner, Repo: src.Repo, Subdir: src.Subdir,
			Ref: *ref, Commit: commit, ManagedPath: final,
		})
	}
	if err != nil {
		_ = os.RemoveAll(final)
		if hadPrevious {
			_ = os.Rename(backup, final)
		}
		return err
	}
	fmt.Fprintf(os.Stderr, "%s installed %s from %s\n", tag(), installed.ID, src)
	fmt.Fprintf(os.Stderr, "%s restart the session's server for a running session to pick it up\n", tag())
	return nil
}

// uninstallPlugin forgets a plugin, by id or by the owner/repo it was
// installed from, and removes its files when tend put them there.
func uninstallPlugin(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tend plugin uninstall <id|owner/repo[/subdir...]>")
	}
	registry, err := openRegistry()
	if err != nil {
		return err
	}
	installed, ok := registry.Get(args[0])
	if !ok {
		if src, err := plugin.ParseGithubSource(args[0]); err == nil {
			installed, ok = registry.ByGithubSource(src)
		}
	}
	if !ok {
		return fmt.Errorf("no plugin %q is installed", args[0])
	}
	if err := registry.Unlink(installed.ID); err != nil {
		return err
	}
	if s := installed.Source; s != nil && s.Kind == "github" && s.ManagedPath != "" {
		root, err := managedCheckouts()
		// Only inside tend's own directory: a path in the registry is not
		// enough reason to delete anything anywhere.
		if err == nil && strings.HasPrefix(filepath.Clean(s.ManagedPath), filepath.Clean(root)+string(os.PathSeparator)) {
			if err := os.RemoveAll(s.ManagedPath); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(os.Stderr, "%s uninstalled %s\n", tag(), installed.ID)
	return nil
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// confirm asks a yes-or-no question on the terminal; anything but yes is no.
func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s %s [y/N] ", tag(), question)
	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
