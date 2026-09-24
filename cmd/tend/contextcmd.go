package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sousaakira/tend/internal/api"
)

// `tend context`, the context buffer from a shell: what a script or a
// person captured goes to the server with add, and list and clear look at
// it and empty it. It is the automation socket's context.* with a shell
// face, as the other commands here are; a browser extension calls the same
// methods on the socket.
func runContext(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tend context add|list|clear [flags]")
	}
	switch args[0] {
	case "add":
		return runContextAdd(args[1:])
	case "list":
		fs := flag.NewFlagSet("context list", flag.ExitOnError)
		name := sessionFlag(fs)
		asJSON := fs.Bool("json", false, "print the items as JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := apiCall(*name, api.MethodContextList, nil, false)
		if err != nil {
			return err
		}
		items, _ := out["items"].([]any)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}
		for _, raw := range items {
			it, _ := raw.(map[string]any)
			fmt.Printf("%v\t%v\t%s\n", it["id"], it["kind"], contextLine(it))
		}
		return nil
	case "clear":
		fs := flag.NewFlagSet("context clear", flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, err := apiCall(*name, api.MethodContextClear, nil, false)
		return err
	}
	return fmt.Errorf("tend context: %q is not add, list or clear", args[0])
}

func runContextAdd(args []string) error {
	fs := flag.NewFlagSet("context add", flag.ExitOnError)
	name := sessionFlag(fs)
	kind := fs.String("kind", "", "url, element, text or file (default: from what is given)")
	url := fs.String("url", "", "a page's address")
	title := fs.String("title", "", "a page's title")
	selector := fs.String("selector", "", "an element's CSS selector")
	elementTag := fs.String("tag", "", "an element's tag")
	file := fs.String("file", "", "a file's path")
	source := fs.String("source", "cli", "what captured it")
	note := fs.String("note", "", "what to do about it, for the agent")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend context add [flags] [text...]\n\n"+
				"puts something in the session's context buffer, for the context\n"+
				"panel (prefix+C) to send to an agent. text left over is the item's\n"+
				"text; - reads it from stdin.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(hoistFlags(args, map[string]bool{"s": true, "kind": true, "url": true, "title": true,
		"selector": true, "tag": true, "file": true, "source": true, "note": true})); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	if text == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = string(raw)
	}
	if *kind == "" {
		switch {
		case *selector != "" || *elementTag != "":
			*kind = "element"
		case *file != "":
			*kind = "file"
		case *url != "" && text == "":
			*kind = "url"
		default:
			*kind = "text"
		}
	}
	if *file != "" && !strings.HasPrefix(*file, "/") {
		if wd, err := os.Getwd(); err == nil {
			*file = wd + "/" + *file
		}
	}
	params := map[string]any{"kind": *kind, "source": *source, "title": *title, "url": *url,
		"selector": *selector, "tag": *elementTag, "path": *file, "text": text, "note": *note}
	out, err := apiCall(*name, api.MethodContextAdd, params, false)
	if err != nil {
		return err
	}
	if it, ok := out["item"].(map[string]any); ok {
		fmt.Fprintf(os.Stderr, "%s added %v: %s\n", tag(), it["id"], contextLine(it))
	}
	return nil
}

func contextLine(it map[string]any) string {
	for _, k := range []string{"selector", "url", "path", "text"} {
		if v, _ := it[k].(string); v != "" {
			return strings.Join(strings.Fields(v), " ")
		}
	}
	return ""
}
