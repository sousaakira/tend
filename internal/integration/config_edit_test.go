package integration

import (
	"path/filepath"
	"testing"
)

func TestEnsureCommandHookIsIdempotent(t *testing.T) {
	hooks := map[string]any{}
	hookPath := filepath.Join("/home", "user", ".claude", "hooks", "tend-agent-state.sh")
	command := HookCommand(hookPath, "session")

	if err := EnsureCommandHook(hooks, "SessionStart", command, 10, ""); err != nil {
		t.Fatalf("first EnsureCommandHook: %v", err)
	}
	if err := EnsureCommandHook(hooks, "SessionStart", command, 10, ""); err != nil {
		t.Fatalf("second EnsureCommandHook: %v", err)
	}

	entries, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatalf("SessionStart hooks = %T, want []any", hooks["SessionStart"])
	}
	if len(entries) != 1 {
		t.Fatalf("len(SessionStart) = %d, want 1", len(entries))
	}

	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry = %T, want map[string]any", entries[0])
	}
	nested, ok := entry["hooks"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("nested hooks = %#v, want one command hook", entry["hooks"])
	}
	if !IsMatchingCommandHook(nested[0], command) {
		t.Fatalf("nested hook = %#v, want command %q", nested[0], command)
	}
}

func TestEnsureCommandHookAddsMatcher(t *testing.T) {
	hooks := map[string]any{}
	command := "bash '/tmp/tend-agent-state.sh' working"

	if err := EnsureCommandHook(hooks, "PreToolUse", command, 10, "*"); err != nil {
		t.Fatalf("EnsureCommandHook: %v", err)
	}

	entry := hooks["PreToolUse"].([]any)[0].(map[string]any)
	if got, _ := entry["matcher"].(string); got != "*" {
		t.Fatalf("matcher = %q, want %q", got, "*")
	}
}

func TestRemoveHookCommandsRemovesNestedHook(t *testing.T) {
	hooks := map[string]any{}
	hookPath := filepath.Join("/tmp", "tend-agent-state.sh")
	command := HookCommand(hookPath, "working")

	if err := EnsureCommandHook(hooks, "UserPromptSubmit", command, 10, ""); err != nil {
		t.Fatalf("EnsureCommandHook: %v", err)
	}

	removed, err := RemoveHookCommands(hooks, "UserPromptSubmit", hookPath, "working")
	if err != nil {
		t.Fatalf("RemoveHookCommands: %v", err)
	}
	if !removed {
		t.Fatal("RemoveHookCommands = false, want true")
	}
	if _, ok := hooks["UserPromptSubmit"]; ok {
		t.Fatalf("UserPromptSubmit still present: %#v", hooks)
	}
}

func TestRemoveHookCommandsLeavesUnrelatedHooks(t *testing.T) {
	hooks := map[string]any{}
	hookPath := filepath.Join("/tmp", "tend-agent-state.sh")
	command := HookCommand(hookPath, "session")
	other := "bash '/tmp/other.sh' session"

	if err := EnsureCommandHook(hooks, "SessionStart", command, 10, ""); err != nil {
		t.Fatalf("EnsureCommandHook tend hook: %v", err)
	}
	if err := EnsureCommandHook(hooks, "SessionStart", other, 10, ""); err != nil {
		t.Fatalf("EnsureCommandHook other hook: %v", err)
	}

	removed, err := RemoveHookCommands(hooks, "SessionStart", hookPath, "session")
	if err != nil {
		t.Fatalf("RemoveHookCommands: %v", err)
	}
	if !removed {
		t.Fatal("RemoveHookCommands = false, want true")
	}

	entries := hooks["SessionStart"].([]any)
	if len(entries) != 1 {
		t.Fatalf("len(SessionStart) = %d, want 1 unrelated hook left", len(entries))
	}
	nested := entries[0].(map[string]any)["hooks"].([]any)
	if !IsMatchingCommandHook(nested[0], other) {
		t.Fatalf("remaining hook = %#v, want %q", nested[0], other)
	}
}

func TestRemoveSimpleCommandHook(t *testing.T) {
	hooks := map[string]any{
		"sessionStart": []any{
			map[string]any{"command": "bash '/tmp/tend-agent-state.sh' session"},
			map[string]any{"command": "bash '/tmp/other.sh' session"},
		},
	}

	removed, err := RemoveSimpleCommandHook(hooks, "sessionStart", "bash '/tmp/tend-agent-state.sh' session")
	if err != nil {
		t.Fatalf("RemoveSimpleCommandHook: %v", err)
	}
	if !removed {
		t.Fatal("RemoveSimpleCommandHook = false, want true")
	}

	entries := hooks["sessionStart"].([]any)
	if len(entries) != 1 {
		t.Fatalf("len(sessionStart) = %d, want 1", len(entries))
	}
}

func TestMarshalJSONPrettyMatchesIndentedObject(t *testing.T) {
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "bash '/tmp/tend-agent-state.sh' session",
							"timeout": float64(10),
						},
					},
				},
			},
		},
	}

	out, err := MarshalJSONPretty(settings)
	if err != nil {
		t.Fatalf("MarshalJSONPretty: %v", err)
	}
	if len(out) == 0 || out[len(out)-1] == '\n' {
		t.Fatalf("MarshalJSONPretty added trailing newline: %q", out)
	}
	if string(out[:1]) != "{" {
		t.Fatalf("MarshalJSONPretty = %q, want indented JSON object", out)
	}
}
