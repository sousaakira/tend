package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/config"
)

// `tend browser`, the session's browsers from a shell (server/browser.go):
// status names the ones attached, open and select tell them what to do, and
// attach makes this terminal one — it prints each command it is sent, one
// JSON line each — which is what the bridge a browser extension talks
// through will do, and how that road is tried before there is an extension.
func runBrowser(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tend browser status|open <url>|select on|off|attach|launch [url]|bridge")
	}
	fs := flag.NewFlagSet("browser "+args[0], flag.ExitOnError)
	name := sessionFlag(fs)
	label := fs.String("name", "terminal", "attach: what to call this browser in status")
	if err := fs.Parse(hoistFlags(args[1:], map[string]bool{"s": true, "name": true})); err != nil {
		return err
	}
	switch args[0] {
	case "status":
		out, err := apiCall(*name, api.MethodBrowserStatus, nil, false)
		if err != nil {
			return err
		}
		browsers, _ := out["browsers"].([]any)
		if len(browsers) == 0 {
			fmt.Println("no browser attached")
		}
		for _, b := range browsers {
			fmt.Println(b)
		}
		return nil
	case "open":
		if fs.NArg() != 1 {
			return errors.New("usage: tend browser open <url>")
		}
		_, err := apiCall(*name, api.MethodBrowserOpen, map[string]any{"url": fs.Arg(0)}, false)
		return err
	case "select":
		on := fs.Arg(0)
		if on != "on" && on != "off" {
			return errors.New("usage: tend browser select on|off")
		}
		_, err := apiCall(*name, api.MethodBrowserSelect, map[string]any{"on": on == "on"}, false)
		return err
	case "attach":
		return browserAttach(*name, *label)
	case "bridge":
		return runBrowserBridge()
	case "launch":
		cfg, _ := config.LoadLenient()
		used, err := launchTendBrowser(*name, remoteHost, cfg.Browser.Program, fs.Arg(0))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s opened %s with tend's extension, for session %q\n", tag(), used, *name)
		return nil
	}
	return fmt.Errorf("tend browser: %q is not status, open, select, attach, launch or bridge", args[0])
}

// browserAttach holds a browser.attach stream open and prints what comes.
func browserAttach(session, label string) error {
	conn, err := dialAPI(session)
	if err != nil {
		return err
	}
	defer conn.Close()
	req, _ := json.Marshal(map[string]any{"id": "attach", "method": api.MethodBrowserAttach,
		"params": map[string]any{"name": label}})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	first, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.Contains(first, "browser_attached") {
		return fmt.Errorf("attach: %s", strings.TrimSpace(first))
	}
	fmt.Fprintf(os.Stderr, "%s attached as %q; commands follow, one a line\n", tag(), label)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil
		}
		fmt.Print(line)
	}
}
