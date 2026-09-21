package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sousaakira/tend/internal/api"
)

// `tend worktree …`: a checkout of its own for an agent, as a space in the
// session. herdr's `herdr worktree create|list|open|remove`.
func runWorktree(args []string) error {
	usage := func(w io.Writer) {
		fmt.Fprint(w,
			"usage: tend worktree <command> [options]\n\n"+
				"  list                  the repository's worktrees, and which are open\n"+
				"  create [branch]       a new worktree, opened as a space\n"+
				"  open <branch|path>    open an existing worktree as a space\n"+
				"  remove <space>        remove a worktree and close its space\n\n"+
				"the repository is the one the current directory is in, or -space's.\n\n")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no command given")
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("worktree "+sub, flag.ExitOnError)
	name := sessionFlag(fs)
	space := fs.String("space", "", "the space whose repository to use (default: the current directory's)")
	base := fs.String("base", "", "what a new branch starts from (default: HEAD)")
	path := fs.String("path", "", "where to put the checkout (default: from the settings)")
	label := fs.String("label", "", "the space's name (default: the branch)")
	force := fs.Bool("force", false, "remove even with changes that would be lost")
	asJSON := fs.Bool("json", false, "print the raw result")
	trust := fs.Bool("trust", false, "trust a repository owned by another user, for this command only (git's safe.directory)")
	rest = hoistFlags(rest, map[string]bool{
		"space": true, "base": true, "path": true, "label": true,
		"force": false, "json": false, "trust": false, "s": true, "session": true,
	})
	if err := fs.Parse(rest); err != nil {
		return err
	}

	source := map[string]any{}
	if *trust {
		source["trust_repository"] = true
	}
	if *space != "" {
		source["workspace_id"] = *space
	} else if wd, err := os.Getwd(); err == nil {
		source["cwd"] = wd
	}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range source {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	var (
		result map[string]any
		err    error
	)
	switch sub {
	case "list":
		result, err = apiCall(*name, api.MethodWorktreeList, source, false)
		if err == nil && !*asJSON {
			return printWorktrees(result)
		}
	case "create":
		params := with(map[string]any{"base": *base, "path": *path, "label": *label})
		if fs.NArg() > 0 {
			params["branch"] = fs.Arg(0)
		}
		result, err = apiCall(*name, api.MethodWorktreeCreate, params, true)
	case "open":
		if fs.NArg() == 0 {
			return errors.New("usage: tend worktree open <branch|path>")
		}
		target := fs.Arg(0)
		params := with(map[string]any{"label": *label})
		if len(target) > 0 && (target[0] == '/' || target[0] == '~') {
			params["path"] = target
		} else {
			params["branch"] = target
		}
		result, err = apiCall(*name, api.MethodWorktreeOpen, params, false)
	case "remove":
		if fs.NArg() == 0 {
			return errors.New("usage: tend worktree remove <space>")
		}
		result, err = apiCall(*name, api.MethodWorktreeRemove, map[string]any{
			"workspace_id": fs.Arg(0), "force": *force, "trust_repository": *trust,
		}, true)
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", sub)
	}
	if err != nil {
		return err
	}
	if *asJSON || sub != "list" {
		if !*asJSON {
			describeWorktreeResult(result)
			return nil
		}
		return printJSON(result)
	}
	return nil
}

// describeWorktreeResult says in a line what happened.
func describeWorktreeResult(result map[string]any) {
	switch text(result["type"]) {
	case "worktree_removed":
		fmt.Fprintf(os.Stderr, "%s removed %s and closed %s\n", tag(), text(result["path"]), text(result["workspace_id"]))
	default:
		wt, _ := result["worktree"].(map[string]any)
		ws, _ := result["workspace"].(map[string]any)
		verb := "opened"
		if result["already_open"] == true {
			verb = "already open in"
		} else if text(result["type"]) == "worktree_created" {
			verb = "created, opened in"
		}
		fmt.Fprintf(os.Stderr, "%s %s %s %s (%s)\n", tag(), text(wt["branch"]), verb,
			text(ws["workspace_id"]), text(wt["path"]))
	}
}

func printWorktrees(result map[string]any) error {
	list, _ := result["worktrees"].([]any)
	t := newTable("BRANCH", "SPACE", "PATH")
	for _, raw := range list {
		wt, _ := raw.(map[string]any)
		branch := text(wt["branch"])
		if branch == "" {
			branch = "(detached)"
		}
		space := text(wt["open_workspace_id"])
		if space == "" {
			space = "-"
		}
		t.row(branch, space, text(wt["path"]))
	}
	return t.flush()
}
