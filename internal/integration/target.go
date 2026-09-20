package integration

import (
	"fmt"
	"strings"
)

// Target names an agent integration tend can install hooks for.
type Target string

const (
	TargetPi             Target = "pi"
	TargetOmp            Target = "omp"
	TargetClaude         Target = "claude"
	TargetCodex          Target = "codex"
	TargetCopilot        Target = "copilot"
	TargetDevin          Target = "devin"
	TargetDroid          Target = "droid"
	TargetKimi           Target = "kimi"
	TargetOpencode       Target = "opencode"
	TargetKilo           Target = "kilo"
	TargetHermes         Target = "hermes"
	TargetQoderCLI       Target = "qodercli"
	TargetQwen           Target = "qwen"
	TargetCursor         Target = "cursor"
	TargetMastracode     Target = "mastracode"
	TargetAntigravityCLI Target = "antigravity-cli"
	TargetGrok           Target = "grok"
	TargetLetta          Target = "letta"
)

var allTargets = []Target{
	TargetPi,
	TargetOmp,
	TargetClaude,
	TargetCodex,
	TargetCopilot,
	TargetDevin,
	TargetDroid,
	TargetKimi,
	TargetOpencode,
	TargetKilo,
	TargetHermes,
	TargetQoderCLI,
	TargetQwen,
	TargetCursor,
	TargetMastracode,
	TargetAntigravityCLI,
	TargetGrok,
	TargetLetta,
}

// AllTargets returns every integration target in stable order.
func AllTargets() []Target {
	out := make([]Target, len(allTargets))
	copy(out, allTargets)
	return out
}

// ParseTarget converts a CLI/API label into a Target.
func ParseTarget(raw string) (Target, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	for _, target := range allTargets {
		if string(target) == normalized {
			return target, nil
		}
	}
	return "", fmt.Errorf("integration: unknown target %q", raw)
}

// Label returns the stable string label for target.
func (target Target) Label() string {
	return string(target)
}

// Commands returns the binaries tend uses to detect whether the agent is
// installed. The first name is the primary command.
func (target Target) Commands() []string {
	switch target {
	case TargetPi:
		return []string{"pi"}
	case TargetOmp:
		return []string{"omp"}
	case TargetClaude:
		return []string{"claude"}
	case TargetCodex:
		return []string{"codex"}
	case TargetCopilot:
		return []string{"copilot"}
	case TargetDevin:
		return []string{"devin"}
	case TargetDroid:
		return []string{"droid"}
	case TargetKimi:
		return []string{"kimi"}
	case TargetOpencode:
		return []string{"opencode"}
	case TargetKilo:
		return []string{"kilo", "kilo-code"}
	case TargetHermes:
		return []string{"hermes"}
	case TargetQoderCLI:
		return []string{"qodercli"}
	case TargetQwen:
		return []string{"qwen"}
	case TargetCursor:
		return []string{"cursor-agent"}
	case TargetMastracode:
		return []string{"mastracode"}
	case TargetAntigravityCLI:
		return []string{"agy"}
	case TargetGrok:
		return []string{"grok"}
	case TargetLetta:
		return []string{"letta"}
	default:
		return nil
	}
}

// Supported reports whether tend can install hooks for target on this platform.
// Every target is supported on Unix; a Windows port will narrow this set.
func (target Target) Supported() bool {
	_ = target
	return true
}
