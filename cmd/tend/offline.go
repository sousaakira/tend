package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/vt"
)

// defaultSize is the terminal the offline commands parse into. Detection reads
// line-counted regions, so the height a capture is replayed at changes what a
// rule sees; it is a flag rather than a constant for that reason.
const (
	defaultCols = 120
	defaultRows = 40
)

func runAgents(args []string) error {
	fs := flag.NewFlagSet("agents", flag.ExitOnError)
	verbose := fs.Bool("v", false, "show rule counts and aliases")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: tend agents [-v]\n\nlists the agents tend can detect.\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	catalog, err := detect.Bundled()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if *verbose {
		fmt.Fprintln(w, "AGENT\tRULES\tVERSION\tALIASES")
	}
	for _, m := range catalog.Manifests() {
		if *verbose {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\n",
				m.ID, len(m.Rules), orDash(m.Version), orDash(strings.Join(m.Aliases, ", ")))
		} else {
			fmt.Fprintln(w, m.ID)
		}
	}
	return w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// runDetect replays a capture of raw terminal output and reports the state it
// implies. The input is raw bytes as a program wrote them, escape sequences
// included, which is what `tend watch -capture` and script(1) produce.
func runDetect(args []string, explain bool) error {
	name := "detect"
	if explain {
		name = "explain"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	agentName := fs.String("agent", "", "agent manifest to use (required)")
	cols := fs.Int("cols", defaultCols, "terminal width to replay the capture at")
	rows := fs.Int("rows", defaultRows, "terminal height to replay the capture at")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(),
			"usage: tend %s -agent NAME [file]\n\n"+
				"replays raw terminal output and reports the agent state it implies.\n"+
				"reads stdin when no file is given, or when the file is \"-\".\n\n", name)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *agentName == "" {
		return fmt.Errorf("-agent is required; run \"tend agents\" to list them")
	}

	catalog, err := detect.Bundled()
	if err != nil {
		return err
	}
	manifest, ok := catalog.Lookup(*agentName)
	if !ok {
		return fmt.Errorf("unknown agent %q; run \"tend agents\" to list them", *agentName)
	}

	screen, err := replay(fs.Arg(0), *cols, *rows)
	if err != nil {
		return err
	}
	in := agent.Snapshot(screen)

	if !explain {
		res := manifest.Detect(in)
		printResult(manifest.ID, res)
		return nil
	}

	res, evals := manifest.Explain(in)
	printResult(manifest.ID, res)
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "     RULE\tSTATE\tPRIORITY\tREGION\tREGION BYTES")
	for _, e := range evals {
		mark := "   "
		if e.Matched {
			mark = " ✓ "
		}
		fmt.Fprintf(w, "%s  %s\t%s\t%d\t%s\t%d\n",
			mark, e.RuleID, e.State, e.Priority, e.Region, e.RegionBytes)
	}
	return w.Flush()
}

func printResult(agentID string, res detect.Result) {
	fmt.Printf("agent  %s\n", agentID)
	fmt.Printf("state  %s\n", res.State)
	if !res.Matched {
		fmt.Println("rule   (no rule matched: " + res.FallbackReason + ")")
		return
	}
	fmt.Printf("rule   %s (priority %d, region %s)\n", res.RuleID, res.Priority, res.Region)

	var flags []string
	if res.VisibleIdle || res.VisibleBlocker || res.VisibleWorking {
		flags = append(flags, "visible")
	}
	if res.SkipStateUpdate {
		flags = append(flags, "skip-state-update")
	}
	if len(flags) > 0 {
		fmt.Printf("flags  %s\n", strings.Join(flags, ", "))
	}
}

// runScreen shows what the terminal core made of a capture, which is the first
// thing to check when a rule does not fire: the rules can only be as right as
// the screen they read.
func runScreen(args []string) error {
	fs := flag.NewFlagSet("screen", flag.ExitOnError)
	cols := fs.Int("cols", defaultCols, "terminal width")
	rows := fs.Int("rows", defaultRows, "terminal height")
	numbers := fs.Bool("n", false, "number the rows")
	detection := fs.Bool("detection", false, "show the text detection reads, not the raw screen")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(),
			"usage: tend screen [file]\n\n"+
				"renders raw terminal output as tend parses it.\n"+
				"reads stdin when no file is given, or when the file is \"-\".\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	screen, err := replay(fs.Arg(0), *cols, *rows)
	if err != nil {
		return err
	}

	if *detection {
		fmt.Println(agent.ScreenText(screen))
	} else {
		g := screen.Grid()
		for y := 0; y < g.Rows(); y++ {
			if *numbers {
				fmt.Printf("%3d │ %s\n", y+1, g.Line(y).Text())
			} else {
				fmt.Println(g.Line(y).Text())
			}
		}
	}

	cur := screen.Cursor()
	fmt.Fprintf(os.Stderr, "\n-- %dx%d, cursor (%d,%d), alt=%v, history=%d\n",
		g0(screen), g1(screen), cur.X, cur.Y, screen.IsAlt(), screen.MainGrid().HistoryLen())
	if t := screen.Title(); t != "" {
		fmt.Fprintf(os.Stderr, "-- title: %s\n", t)
	}
	if p := screen.Progress(); p != "" {
		fmt.Fprintf(os.Stderr, "-- progress: %s\n", p)
	}
	return nil
}

func g0(s *vt.Screen) int { c, _ := s.Size(); return c }
func g1(s *vt.Screen) int { _, r := s.Size(); return r }

// replay feeds a capture through a fresh terminal.
func replay(path string, cols, rows int) (*vt.Screen, error) {
	var r io.Reader = os.Stdin
	if path != "" && path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	screen := vt.NewScreen(cols, rows, 1000)
	if _, err := io.Copy(screen, r); err != nil {
		return nil, fmt.Errorf("reading capture: %w", err)
	}
	return screen, nil
}
