package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/session"
)

// runSession starts a server with one pane per command and prints their state
// as it changes.
//
// It is not the TUI: panes are not drawn, only reported. What it demonstrates
// is the part underneath — several agents running at once in a server that
// watches all of them, which is the thing the client will later attach to.
func runSession(args []string) error {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	interval := fs.Duration("interval", 150*time.Millisecond, "how often panes are re-examined")
	cols := fs.Int("cols", 120, "pane width")
	rows := fs.Int("rows", 40, "pane height")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend session [options] -- command [args...] [-- command [args...]]...\n\n"+
				"runs each command in its own pane and reports their agent state as it\n"+
				"changes. panes are not drawn; this shows the server, not the client.\n\n"+
				"examples:\n"+
				"  tend session -- claude -- codex\n"+
				"  tend session -- claude -- sh -c 'while true; do date; sleep 1; done'\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	commands := splitCommands(fs.Args())
	if len(commands) == 0 {
		fs.Usage()
		return fmt.Errorf("no commands given")
	}

	srv, err := server.New(server.Config{
		Build:          version,
		DetectInterval: *interval,
		DefaultSize:    pty.Size{Cols: uint16(*cols), Rows: uint16(*rows)},
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	sub := srv.Subscribe(256)
	defer sub.Close()

	names, err := openPanes(srv, commands)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "%s %d panes running. ctrl-c to stop.\n\n", tag(), len(names))

	// Ctrl-C closes the server, which hangs up every pane and ends the event
	// stream, which ends the loop below. One shutdown path, not two.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	go func() {
		<-interrupt
		fmt.Fprintf(os.Stderr, "\n%s stopping...\n", tag())
		_ = srv.Close()
	}()

	printPanes(srv, names)
	for ev := range sub.C {
		// Output events arrive per read and say nothing new about state, so
		// they would reprint the table continuously for no reason.
		if ev.Kind == server.EventPaneOutput {
			continue
		}
		printPanes(srv, names)
		if ev.Kind == server.EventPaneExited {
			fmt.Fprintf(os.Stderr, "%s pane %d exited%s\n", tag(), ev.Pane, errSuffix(ev.Err))
		}
	}
	return nil
}

// splitCommands breaks "a b -- c d" into [[a b] [c d]].
func splitCommands(args []string) [][]string {
	var out [][]string
	var current []string
	for _, arg := range args {
		if arg == "--" {
			if len(current) > 0 {
				out = append(out, current)
				current = nil
			}
			continue
		}
		current = append(current, arg)
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

// openPanes puts the first command in a new tab and splits for the rest.
func openPanes(srv *server.Server, commands [][]string) (map[session.PaneID]string, error) {
	ws, err := srv.NewWorkspace("main")
	if err != nil {
		return nil, err
	}

	names := make(map[session.PaneID]string, len(commands))
	_, first, err := srv.NewTab(ws, "main", server.PaneSpec{Command: commands[0]})
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", commands[0][0], err)
	}
	names[first] = filepath.Base(commands[0][0])

	previous := first
	for _, cmd := range commands[1:] {
		// Alternating directions keeps a long list of panes from becoming one
		// unreadable strip.
		dir := session.Columns
		if len(names)%2 == 0 {
			dir = session.Rows
		}
		id, err := srv.SplitPane(previous, dir, server.PaneSpec{Command: cmd})
		if err != nil {
			return nil, fmt.Errorf("starting %s: %w", cmd[0], err)
		}
		names[id] = filepath.Base(cmd[0])
		previous = id
	}
	return names, nil
}

// printPanes reprints the pane table.
func printPanes(srv *server.Server, names map[session.PaneID]string) {
	statuses := srv.Statuses()
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })

	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tCOMMAND\tAGENT\tSTATE\tTITLE")
	for _, st := range statuses {
		state := st.State.String()
		if !st.Running {
			state = "exited"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s%s%s\t%s\n",
			st.ID, orDash(names[st.ID]), orDash(st.Agent),
			stateColorFor(st), state, reset(), orDash(st.Title))
	}
	_ = w.Flush()
	fmt.Print(b.String())
	fmt.Println()
}

func stateColorFor(st server.PaneStatus) string {
	if !st.Running {
		if !colorsEnabled() {
			return ""
		}
		return "\x1b[2m"
	}
	return stateColor(st.State)
}

func errSuffix(err string) string {
	if err == "" {
		return ""
	}
	return ": " + err
}
