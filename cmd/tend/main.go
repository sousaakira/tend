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

usage: tend <command> [options]

commands:
  agents            list the agents tend can detect
  detect            report an agent's state from captured terminal output
  explain           show how every rule voted, for one capture
  screen            render captured terminal output as tend parses it
  watch             run a command on a pty and report its agent state
  version           print the version

run "tend <command> -h" for a command's options.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch cmd := os.Args[1]; cmd {
	case "agents":
		err = runAgents(os.Args[2:])
	case "detect":
		err = runDetect(os.Args[2:], false)
	case "explain":
		err = runDetect(os.Args[2:], true)
	case "screen":
		err = runScreen(os.Args[2:])
	case "watch":
		err = runWatch(os.Args[2:])
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
