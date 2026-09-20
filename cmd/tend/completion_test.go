package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestCompletionKnowsEveryCommand: the scripts are generated from one table, so
// the only way for them to go stale is for that table to. A command added to
// the dispatcher and not to it would otherwise be found by somebody pressing
// tab and getting nothing.
func TestCompletionKnowsEveryCommand(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	listed := make(map[string]bool)
	for _, c := range commands {
		listed[c.name] = true
	}
	for _, m := range regexp.MustCompile(`case "([a-z]+)":`).FindAllStringSubmatch(string(src), -1) {
		if name := m[1]; name != "help" && !listed[name] {
			t.Errorf("%q is dispatched in main.go but missing from the completion table", name)
		}
	}
	for _, c := range commands {
		if !strings.Contains(string(src), `case "`+c.name+`"`) {
			t.Errorf("%q is completed but main.go does not dispatch it", c.name)
		}
		if !strings.Contains(usage, c.name) {
			t.Errorf("%q is completed but the usage text does not mention it", c.name)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, shell := range completionShells {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		for _, c := range commands {
			if !strings.Contains(script, c.name) {
				t.Errorf("%s completion does not offer %q", shell, c.name)
			}
		}
		if !strings.Contains(script, "ls -sessions") {
			t.Errorf("%s completion should offer session names after -s", shell)
		}
	}
	if _, err := completionScript("tcsh"); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Errorf("an unknown shell should be refused with the list of known ones, got %v", err)
	}
}

// TestCompletionScriptsParse runs each script through its own shell's syntax
// check, which is the difference between a script that looks right and one a
// user can actually source.
func TestCompletionScriptsParse(t *testing.T) {
	checks := map[string][]string{
		"bash": {"bash", "-n"},
		"zsh":  {"zsh", "-n"},
		"fish": {"fish", "--no-execute"},
	}
	for shell, argv := range checks {
		if _, err := exec.LookPath(argv[0]); err != nil {
			t.Logf("%s is not installed; its script is not syntax-checked", shell)
			continue
		}
		script, _ := completionScript(shell)
		path := t.TempDir() + "/completion." + shell
		if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(argv[0], append(argv[1:], path)...).CombinedOutput(); err != nil {
			t.Errorf("%s rejects its completion script: %v\n%s", shell, err, out)
		}
	}
}
