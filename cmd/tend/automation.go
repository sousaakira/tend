package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/transport"
)

// The commands a script uses, and the ones a person types when driving an
// agent from another terminal. They are the automation socket with a shell
// face on it: every one of them is a single call, and `tend api` is the same
// door with nothing in the way, for the methods that have no command of their
// own yet.
//
// herdr's shape: `herdr agent prompt <target> <text> --wait`, `herdr agent
// read <target>`, `herdr pane send-keys <pane> <key>...`.

// hoistFlags moves flags that follow the positional arguments in front of
// them, so `tend agent prompt claude do the thing -wait` means what it looks
// like.
//
// Go's flag package stops at the first argument that is not a flag, which for
// these commands is the target — so everything after it, including -wait, was
// read as part of the prompt and silently dropped. A real prompt was sent
// without its wait because of exactly that. Only the names given are moved, so
// a prompt may still contain a word starting with a dash, and "--" stops the
// scan for a prompt that has to contain one of these names.
func hoistFlags(args []string, valued map[string]bool) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		known, isFlag := valued[name]
		if !isFlag || !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		flags = append(flags, arg)
		if known && !hasValue && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
		_ = value
	}
	return append(flags, rest...)
}

// apiTimeout bounds a call that is not a wait. A server that has stopped
// answering must fail a script rather than hang it.
const apiTimeout = 30 * time.Second

// dialAPI connects to a session's automation socket.
func dialAPI(session string) (net.Conn, error) {
	path, err := transport.APISocketPath(session)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("session %q is not running, or its server predates the automation socket", sessionName(session))
		}
		return nil, fmt.Errorf("connecting to %s: %w", path, err)
	}
	return conn, nil
}

func sessionName(s string) string {
	if s == "" {
		return transport.DefaultSessionName
	}
	return s
}

// apiCall makes one call and returns its result object.
//
// wait says the call is one that blocks on purpose — an agent finishing, an
// event arriving — so the deadline is the caller's to set, not this one's.
func apiCall(session, method string, params map[string]any, wait bool) (map[string]any, error) {
	conn, err := dialAPI(session)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if !wait {
		_ = conn.SetDeadline(time.Now().Add(apiTimeout))
	}
	req := map[string]any{"id": "cli", "method": method}
	if len(params) > 0 {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}

	reply, err := bufio.NewReaderSize(conn, 1<<16).ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("the server did not answer: %w", err)
	}
	var out struct {
		Result map[string]any `json:"result"`
		Error  *api.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		return nil, fmt.Errorf("the server's answer could not be read: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

// printJSON writes a result the way a script reads it: one object, one line,
// so `tend ... | jq` works without buffering games.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

// runAPI is the escape hatch: any method, params as JSON.
func runAPI(args []string) error {
	fs := flag.NewFlagSet("api", flag.ExitOnError)
	name := sessionFlag(fs)
	wait := fs.Bool("wait", false, "the call blocks on purpose; do not time it out")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend api <method> [params-json]\n\n"+
				"calls the session's automation socket and prints the result as JSON.\n"+
				"params may be given as an argument, or on stdin when it is \"-\".\n\n"+
				"  tend api session.snapshot\n"+
				"  tend api agent.prompt '{\"target\":\"claude\",\"text\":\"run the tests\"}'\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("no method given")
	}

	params := map[string]any{}
	if fs.NArg() > 1 {
		raw := fs.Arg(1)
		if raw == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			raw = string(data)
		}
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &params); err != nil {
				return fmt.Errorf("params: %w", err)
			}
		}
	}

	result, err := apiCall(*name, fs.Arg(0), params, *wait)
	if err != nil {
		return err
	}
	return printJSON(result)
}

// runAgent drives an agent: list, get, read, prompt, send-keys, wait.
func runAgent(args []string) error {
	usage := func(w io.Writer) {
		fmt.Fprint(w,
			"usage: tend agent <command> [options]\n\n"+
				"  list                       every pane running an agent\n"+
				"  get <target>               one agent's state\n"+
				"  read <target>              what the agent's pane has shown\n"+
				"  prompt <target> <text>     submit a prompt, optionally waiting for the answer\n"+
				"  send-keys <target> <key>…  press keys in the agent's pane\n"+
				"  wait <target>              wait until the agent is idle or blocked\n\n"+
				"a target is a pane (\"p_2\", or \"2\") or an agent name when only one pane runs it.\n\n")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no command given")
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "list":
		fs := flag.NewFlagSet("agent list", flag.ExitOnError)
		name := sessionFlag(fs)
		asJSON := fs.Bool("json", false, "print the raw result")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		result, err := apiCall(*name, api.MethodAgentList, nil, false)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(result)
		}
		return printAgents(result)

	case "get":
		return oneTarget(rest, api.MethodAgentGet, "agent get")

	case "read":
		fs := flag.NewFlagSet("agent read", flag.ExitOnError)
		name := sessionFlag(fs)
		source := fs.String("source", "recent", "recent or visible")
		lines := fs.Int("lines", 0, "how many lines to return (0 means all that are kept)")
		asJSON := fs.Bool("json", false, "print the raw result")
		rest = hoistFlags(rest, map[string]bool{"json": false, "source": true, "lines": true, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("name an agent or a pane to read")
		}
		result, err := apiCall(*name, api.MethodAgentRead, map[string]any{
			"target": fs.Arg(0), "source": *source, "lines": *lines,
		}, false)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(result)
		}
		text, _ := result["text"].(string)
		fmt.Println(text)
		return nil

	case "prompt":
		fs := flag.NewFlagSet("agent prompt", flag.ExitOnError)
		name := sessionFlag(fs)
		wait := fs.Bool("wait", false, "wait for the agent to finish answering")
		timeout := fs.Duration("timeout", 0, "how long to wait (default: the server's)")
		asJSON := fs.Bool("json", false, "print the raw result")
		rest = hoistFlags(rest, map[string]bool{"wait": false, "json": false, "timeout": true, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() < 2 {
			return errors.New("usage: tend agent prompt <target> <text>")
		}
		params := map[string]any{
			"target": fs.Arg(0),
			// Everything after the target, so a prompt does not have to be
			// quoted as one word.
			"text": strings.Join(fs.Args()[1:], " "),
			"wait": *wait,
		}
		if *timeout > 0 {
			params["timeout_ms"] = timeout.Milliseconds()
		}
		result, err := apiCall(*name, api.MethodAgentPrompt, params, *wait)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(result)
		}
		if *wait {
			return printWaited(result)
		}
		fmt.Fprintf(os.Stderr, "%s prompt sent\n", tag())
		return nil

	case "send-keys":
		fs := flag.NewFlagSet("agent send-keys", flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() < 2 {
			return errors.New("usage: tend agent send-keys <target> <key>...")
		}
		_, err := apiCall(*name, api.MethodAgentSendKeys, map[string]any{
			"target": fs.Arg(0), "keys": fs.Args()[1:],
		}, false)
		return err

	case "wait":
		fs := flag.NewFlagSet("agent wait", flag.ExitOnError)
		name := sessionFlag(fs)
		until := fs.String("until", "", "states to wait for, comma separated (default: idle,blocked)")
		timeout := fs.Duration("timeout", 0, "how long to wait (default: the server's)")
		asJSON := fs.Bool("json", false, "print the raw result")
		rest = hoistFlags(rest, map[string]bool{"json": false, "until": true, "timeout": true, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("name an agent or a pane to wait for")
		}
		params := map[string]any{"target": fs.Arg(0)}
		if *until != "" {
			params["until"] = strings.Split(*until, ",")
		}
		if *timeout > 0 {
			params["timeout_ms"] = timeout.Milliseconds()
		}
		result, err := apiCall(*name, api.MethodAgentWait, params, true)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(result)
		}
		return printWaited(result)
	}

	usage(os.Stderr)
	return fmt.Errorf("unknown command %q", sub)
}

// runPane is the same for panes that are not agents.
func runPane(args []string) error {
	usage := func(w io.Writer) {
		fmt.Fprint(w,
			"usage: tend pane <command> [options]\n\n"+
				"  list                    every pane\n"+
				"  get <pane>              one pane\n"+
				"  read <pane>             what the pane has shown\n"+
				"  send-text <pane> <text> type text into the pane\n"+
				"  send-keys <pane> <key>… press keys in the pane\n"+
				"  wait <pane>             wait for the pane to print something\n\n")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no command given")
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "list":
		fs := flag.NewFlagSet("pane list", flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		result, err := apiCall(*name, api.MethodPaneList, nil, false)
		if err != nil {
			return err
		}
		return printJSON(result)

	case "get":
		return oneTarget(rest, api.MethodPaneGet, "pane get")

	case "read":
		fs := flag.NewFlagSet("pane read", flag.ExitOnError)
		name := sessionFlag(fs)
		source := fs.String("source", "recent", "recent or visible")
		lines := fs.Int("lines", 0, "how many lines to return (0 means all that are kept)")
		rest = hoistFlags(rest, map[string]bool{"source": true, "lines": true, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("name a pane to read")
		}
		result, err := apiCall(*name, api.MethodPaneRead, map[string]any{
			"pane_id": fs.Arg(0), "source": *source, "lines": *lines,
		}, false)
		if err != nil {
			return err
		}
		text, _ := result["text"].(string)
		fmt.Println(text)
		return nil

	case "send-text":
		fs := flag.NewFlagSet("pane send-text", flag.ExitOnError)
		name := sessionFlag(fs)
		submit := fs.Bool("submit", false, "press enter after the text")
		rest = hoistFlags(rest, map[string]bool{"submit": false, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() < 2 {
			return errors.New("usage: tend pane send-text <pane> <text>")
		}
		_, err := apiCall(*name, api.MethodPaneSendText, map[string]any{
			"pane_id": fs.Arg(0), "text": strings.Join(fs.Args()[1:], " "), "submit": *submit,
		}, false)
		return err

	case "send-keys":
		fs := flag.NewFlagSet("pane send-keys", flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() < 2 {
			return errors.New("usage: tend pane send-keys <pane> <key>...")
		}
		_, err := apiCall(*name, api.MethodPaneSendKeys, map[string]any{
			"pane_id": fs.Arg(0), "keys": fs.Args()[1:],
		}, false)
		return err

	case "wait":
		fs := flag.NewFlagSet("pane wait", flag.ExitOnError)
		name := sessionFlag(fs)
		contains := fs.String("contains", "", "wait for this text rather than any output")
		timeout := fs.Duration("timeout", 0, "how long to wait (default: the server's)")
		rest = hoistFlags(rest, map[string]bool{"contains": true, "timeout": true, "s": true, "session": true})
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("name a pane to wait for")
		}
		params := map[string]any{"pane_id": fs.Arg(0), "contains": *contains}
		if *timeout > 0 {
			params["timeout_ms"] = timeout.Milliseconds()
		}
		result, err := apiCall(*name, api.MethodPaneWaitForOutput, params, true)
		if err != nil {
			return err
		}
		if result["timed_out"] == true {
			return errors.New("timed out")
		}
		return nil
	}

	usage(os.Stderr)
	return fmt.Errorf("unknown command %q", sub)
}

// oneTarget serves the commands that take a target and print the result.
func oneTarget(args []string, method, what string) error {
	fs := flag.NewFlagSet(what, flag.ExitOnError)
	name := sessionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: tend %s <target>", what)
	}
	key := "target"
	if method == api.MethodPaneGet {
		key = "pane_id"
	}
	result, err := apiCall(*name, method, map[string]any{key: fs.Arg(0)}, false)
	if err != nil {
		return err
	}
	return printJSON(result)
}

// printAgents is the list a person reads, as `tend ls` prints panes.
func printAgents(result map[string]any) error {
	agents, _ := result["agents"].([]any)
	if len(agents) == 0 {
		fmt.Fprintf(os.Stderr, "%s no agents in this session\n", tag())
		return nil
	}
	w := newTable("PANE", "AGENT", "STATE", "TITLE")
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		w.row(text(a["pane_id"]), text(a["agent"]), text(a["agent_state"]), text(a["title"]))
	}
	return w.flush()
}

// printWaited says how a wait ended, and fails the command when it timed out
// so a shell script can tell without parsing anything.
func printWaited(result map[string]any) error {
	info, _ := result["agent"].(map[string]any)
	state := text(info["agent_state"])
	if result["timed_out"] == true {
		waited := ""
		if ms, ok := result["waited_ms"].(float64); ok {
			waited = " after " + (time.Duration(ms) * time.Millisecond).String()
		}
		return fmt.Errorf("timed out%s; the agent is %s", waited, state)
	}
	fmt.Println(state)
	return nil
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// table prints aligned columns, as the other listing commands do.
type table struct {
	head []string
	rows [][]string
}

func newTable(head ...string) *table { return &table{head: head} }

func (t *table) row(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) flush() error {
	widths := make([]int, len(t.head))
	for i, h := range t.head {
		widths[i] = len(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	print := func(cells []string) {
		var b strings.Builder
		for i, c := range cells {
			if i == len(cells)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", widths[i]-len(c)+2))
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
	print(t.head)
	for _, r := range t.rows {
		print(r)
	}
	return nil
}
