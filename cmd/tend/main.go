// Command tend is the tend CLI.
//
// The subcommands here exercise the pieces that exist: the terminal core, and
// manifest-driven agent detection. The server and the TUI client are not built
// yet, so nothing here manages a session — `watch` runs one command and reports
// what its terminal says about it.
package main

import (
	"fmt"
	"os"
)

const version = "tend (development build)"

const usage = `tend — terminal runtime for coding agents

usage: tend [command] [options]

with no command, tend opens a session — starting a server for it if there is
not one already — and draws it.

commands:
  attach            open a session and draw it (the default)
  ls                list a session's panes, or every session
  new               open a pane in a session
  kill              close panes, or stop a session
  follow            print a session's events, for diagnosis
  serve             run a session server in the foreground

commands that need no server:
  agents            list the agents tend can detect
  detect            report an agent's state from captured terminal output
  explain           show how every rule voted, for one capture
  screen            render captured terminal output as tend parses it
  watch             run a command on a pty and report its agent state
  session           run several commands as panes in one process
  version           print the version

run "tend <command> -h" for a command's options.
`

func main() {
	// Bare "tend" opens a session, starting a server if there is none. Asking
	// someone to run a daemon before they can use the thing is a step that
	// exists only because of how it is built.
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"attach"}
	}

	var err error
	switch cmd := args[0]; cmd {
	case "agents":
		err = runAgents(args[1:])
	case "detect":
		err = runDetect(args[1:], false)
	case "explain":
		err = runDetect(args[1:], true)
	case "screen":
		err = runScreen(args[1:])
	case "watch":
		err = runWatch(args[1:])
	case "session":
		err = runSession(args[1:])
	case "serve":
		err = runServe(args[1:])
	case "ls":
		err = runList(args[1:])
	case "new":
		err = runNew(args[1:])
	case "kill":
		err = runKill(args[1:])
	case "attach":
		err = runAttach(args[1:])
	case "follow":
		err = runFollow(args[1:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "tend: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tend: %v\n", err)
		os.Exit(1)
	}
}
