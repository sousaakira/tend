package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/auth-com-br/tend/internal/integration"
)

// runIntegration installs, removes or lists agent hooks. It does not need a
// running session: hooks live in each agent's own config directory, and the
// session only receives what they later report.
func runIntegration(args []string) error {
	if len(args) == 0 {
		printIntegrationHelp(os.Stderr)
		return fmt.Errorf("usage: tend integration <install|uninstall|status> …")
	}
	switch args[0] {
	case "install":
		return integrationInstall(args[1:])
	case "uninstall":
		return integrationUninstall(args[1:])
	case "status":
		return integrationStatus(args[1:])
	case "-h", "--help", "help":
		printIntegrationHelp(os.Stdout)
		return nil
	default:
		printIntegrationHelp(os.Stderr)
		return fmt.Errorf("unknown integration command %q", args[0])
	}
}

func integrationInstall(args []string) error {
	fs := flag.NewFlagSet("integration install", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: tend integration install <target>\n\n")
		printIntegrationTargets(fs.Output())
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("usage: tend integration install <target>")
	}
	target, err := integration.ParseTarget(fs.Arg(0))
	if err != nil {
		return err
	}
	messages, err := integration.Install(target)
	if err != nil {
		return err
	}
	for _, m := range messages {
		fmt.Println(m)
	}
	return nil
}

func integrationUninstall(args []string) error {
	fs := flag.NewFlagSet("integration uninstall", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: tend integration uninstall <target>\n\n")
		printIntegrationTargets(fs.Output())
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("usage: tend integration uninstall <target>")
	}
	target, err := integration.ParseTarget(fs.Arg(0))
	if err != nil {
		return err
	}
	messages, err := integration.Uninstall(target)
	if err != nil {
		return err
	}
	for _, m := range messages {
		fmt.Println(m)
	}
	return nil
}

func integrationStatus(args []string) error {
	fs := flag.NewFlagSet("integration status", flag.ExitOnError)
	outdatedOnly := fs.Bool("outdated-only", false, "print only a notice when installed hooks are behind")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: tend integration status [--outdated-only]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	statuses := integration.Statuses()
	if *outdatedOnly {
		var outdated []string
		for _, st := range statuses {
			if st.State == integration.StatusOutdated {
				outdated = append(outdated, st.Target.Label())
			}
		}
		if len(outdated) == 0 {
			return nil
		}
		cmds := make([]string, len(outdated))
		for i, label := range outdated {
			cmds[i] = "tend integration install " + label
		}
		fmt.Fprintf(os.Stderr, "installed tend integrations need updating; run %s.\n",
			strings.Join(cmds, ", "))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tSTATE\tPATH")
	for _, st := range statuses {
		fmt.Fprintf(w, "%s\t%s\t%s\n", st.Target.Label(), describeIntegrationState(st), st.Path)
	}
	return w.Flush()
}

func describeIntegrationState(st integration.Status) string {
	switch st.State {
	case integration.StatusNotInstalled:
		return "not installed"
	case integration.StatusCurrent:
		if st.InstalledVersion != nil {
			return fmt.Sprintf("current (v%d)", *st.InstalledVersion)
		}
		return "current"
	case integration.StatusOutdated:
		if st.InstalledVersion != nil {
			return fmt.Sprintf("outdated (v%d < v%d)", *st.InstalledVersion, st.ExpectedVersion)
		}
		return fmt.Sprintf("outdated (legacy < v%d)", st.ExpectedVersion)
	default:
		return string(st.State)
	}
}

func printIntegrationHelp(w io.Writer) {
	fmt.Fprint(w,
		"usage: tend integration <command>\n\n"+
			"installs hooks into coding agents so they report their own state\n"+
			"to a tend session, instead of tend reading it from the screen.\n\n"+
			"commands:\n"+
			"  install <target>     write hooks for one agent\n"+
			"  uninstall <target>   remove those hooks\n"+
			"  status               list every target and whether it is current\n\n")
	printIntegrationTargets(w)
}

func printIntegrationTargets(w io.Writer) {
	fmt.Fprintln(w, "targets:")
	for _, t := range integration.AllTargets() {
		fmt.Fprintf(w, "  %s\n", t.Label())
	}
}
