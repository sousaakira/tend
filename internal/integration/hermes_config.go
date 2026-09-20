package integration

import (
	"strings"
	"unicode"
)

// EnsureHermesPluginEnabled adds tend-agent-state to plugins.enabled (or the
// flat plugins list) without a full YAML parser — Hermes configs are small and
// line-oriented, matching herdr's config_edit.rs.
func EnsureHermesPluginEnabled(content string) string {
	return updateHermesEnabledPlugin(content, true)
}

// RemoveHermesPluginEnabled drops tend-agent-state from the plugins list.
func RemoveHermesPluginEnabled(content string) string {
	return updateHermesEnabledPlugin(content, false)
}

func updateHermesEnabledPlugin(content string, enabled bool) string {
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := splitYAMLLines(content)
	pluginsIndex, ok := topLevelYAMLKeyIndex(lines, "plugins")
	if !ok {
		if !enabled {
			return content
		}
		result := strings.TrimRight(content, "\n")
		if result != "" {
			result += "\n"
		}
		result += "plugins:\n  enabled:\n    - " + HermesPluginInstallName + "\n"
		return result
	}

	pluginsEnd := nextTopLevelYAMLKeyIndex(lines, pluginsIndex+1)
	if pluginsEnd < 0 {
		pluginsEnd = len(lines)
	}
	pluginsInlineItems, hasPluginsInline := yamlFlowSequenceItems(yamlKeyValueAtIndent(lines[pluginsIndex], 0, "plugins"))
	enabledIndex := -1
	for i := pluginsIndex + 1; i < pluginsEnd; i++ {
		if yamlKeyAtIndent(lines[i], 2) == "enabled" {
			enabledIndex = i
			break
		}
	}
	flatListStart := -1
	for i := pluginsIndex + 1; i < pluginsEnd; i++ {
		if yamlListItemValueAtIndent(lines[i], 2) != "" {
			flatListStart = i
			break
		}
	}

	if enabledIndex >= 0 {
		if items, ok := yamlFlowSequenceItems(yamlKeyValueAtIndent(lines[enabledIndex], 2, "enabled")); ok {
			existing := -1
			for i, item := range items {
				if yamlScalarValue(item) == HermesPluginInstallName {
					existing = i
					break
				}
			}
			switch {
			case enabled && existing >= 0, !enabled && existing < 0:
				return content
			case enabled:
				items = append([]string{HermesPluginInstallName}, items...)
			default:
				items = append(items[:existing], items[existing+1:]...)
			}
			comment := yamlInlineComment(lines[enabledIndex])
			replacement := hermesEnabledPluginLines(items, comment)
			lines = spliceLines(lines, enabledIndex, enabledIndex+1, replacement)
			return joinYAMLLines(lines, trailingNewline)
		}

		listStart := enabledIndex + 1
		listEnd := pluginsEnd
		for i := listStart; i < pluginsEnd; i++ {
			indent, ok := yamlIndent(lines[i])
			if ok && indent <= 2 && yamlKeyName(lines[i]) != "" {
				listEnd = i
				break
			}
		}
		existing := -1
		for i := listStart; i < listEnd; i++ {
			if yamlListItemMatches(lines[i], HermesPluginInstallName) {
				existing = i
				break
			}
		}
		switch {
		case enabled && existing >= 0, !enabled && existing < 0:
			return content
		case enabled:
			lines = insertLine(lines, listStart, "    - "+HermesPluginInstallName)
		default:
			lines = append(lines[:existing], lines[existing+1:]...)
		}
		return joinYAMLLines(lines, trailingNewline)
	}

	if hasPluginsInline {
		items := pluginsInlineItems
		existing := -1
		for i, item := range items {
			if yamlScalarValue(item) == HermesPluginInstallName {
				existing = i
				break
			}
		}
		switch {
		case enabled && existing >= 0, !enabled && existing < 0:
			return content
		case enabled:
			items = append([]string{HermesPluginInstallName}, items...)
		default:
			items = append(items[:existing], items[existing+1:]...)
		}
		comment := yamlInlineComment(lines[pluginsIndex])
		replacement := hermesFlatPluginLines(items, comment)
		lines = spliceLines(lines, pluginsIndex, pluginsEnd, replacement)
		return joinYAMLLines(lines, trailingNewline)
	}

	if flatListStart >= 0 {
		existing := -1
		for i := pluginsIndex + 1; i < pluginsEnd; i++ {
			if yamlListItemMatchesAtIndent(lines[i], 2, HermesPluginInstallName) {
				existing = i
				break
			}
		}
		switch {
		case enabled && existing >= 0, !enabled && existing < 0:
			return content
		case enabled:
			lines = insertLine(lines, flatListStart, "  - "+HermesPluginInstallName)
		default:
			lines = append(lines[:existing], lines[existing+1:]...)
		}
		return joinYAMLLines(lines, trailingNewline)
	}

	if enabled {
		lines = insertLine(lines, pluginsIndex+1, "  enabled:")
		lines = insertLine(lines, pluginsIndex+2, "    - "+HermesPluginInstallName)
		return joinYAMLLines(lines, trailingNewline)
	}
	return content
}

func hermesFlatPluginLines(items []string, comment string) []string {
	if len(items) == 0 {
		return []string{withYAMLInlineComment("plugins: []", comment)}
	}
	out := []string{withYAMLInlineComment("plugins:", comment)}
	for _, item := range items {
		out = append(out, "  - "+item)
	}
	return out
}

func hermesEnabledPluginLines(items []string, comment string) []string {
	if len(items) == 0 {
		return []string{withYAMLInlineComment("  enabled: []", comment)}
	}
	out := []string{withYAMLInlineComment("  enabled:", comment)}
	for _, item := range items {
		out = append(out, "    - "+item)
	}
	return out
}

func withYAMLInlineComment(line, comment string) string {
	if comment == "" {
		return line
	}
	return line + " " + strings.TrimRight(comment, " \t")
}

func splitYAMLLines(content string) []string {
	if content == "" {
		return nil
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if trailingNewline && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinYAMLLines(lines []string, trailingNewline bool) string {
	result := strings.Join(lines, "\n")
	if trailingNewline || result == "" {
		result += "\n"
	}
	return result
}

func spliceLines(lines []string, start, end int, replacement []string) []string {
	out := make([]string, 0, len(lines)-end+start+len(replacement))
	out = append(out, lines[:start]...)
	out = append(out, replacement...)
	out = append(out, lines[end:]...)
	return out
}

func insertLine(lines []string, index int, line string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:index]...)
	out = append(out, line)
	out = append(out, lines[index:]...)
	return out
}

func topLevelYAMLKeyIndex(lines []string, key string) (int, bool) {
	for i, line := range lines {
		if yamlKeyAtIndent(line, 0) == key {
			return i, true
		}
	}
	return 0, false
}

func nextTopLevelYAMLKeyIndex(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		indent, ok := yamlIndent(lines[i])
		if ok && indent == 0 && yamlKeyName(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func yamlKeyAtIndent(line string, indent int) string {
	got, ok := yamlIndent(line)
	if !ok || got != indent {
		return ""
	}
	return yamlKeyName(line)
}

func yamlKeyValueAtIndent(line string, indent int, key string) string {
	got, ok := yamlIndent(line)
	if !ok || got != indent {
		return ""
	}
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return ""
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	if strings.TrimSpace(parts[0]) != key {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func yamlKeyName(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return ""
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	key := strings.TrimSpace(parts[0])
	return key
}

func yamlIndent(line string) (int, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return 0, false
	}
	return len(line) - len(trimmed), true
}

func yamlListItemValue(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- ") {
		return ""
	}
	return strings.TrimSpace(trimmed[2:])
}

func yamlListItemMatches(line, value string) bool {
	item := yamlListItemValue(line)
	return item != "" && yamlScalarValue(item) == value
}

func yamlListItemValueAtIndent(line string, indent int) string {
	got, ok := yamlIndent(line)
	if !ok || got != indent {
		return ""
	}
	return yamlListItemValue(line)
}

func yamlListItemMatchesAtIndent(line string, indent int, value string) bool {
	item := yamlListItemValueAtIndent(line, indent)
	return item != "" && yamlScalarValue(item) == value
}

func yamlFlowSequenceItems(value string) ([]string, bool) {
	if value == "" {
		return nil, false
	}
	value = strings.TrimSpace(stripYAMLInlineComment(value))
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, false
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return []string{}, true
	}

	var items []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, ch := range inner {
		if quote != 0 {
			current.WriteRune(ch)
			if quote == '"' && ch == '\\' && !escaped {
				escaped = true
				continue
			}
			if ch == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
			current.WriteRune(ch)
		case ',':
			items = append(items, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	if quote != 0 {
		return nil, false
	}
	items = append(items, strings.TrimSpace(current.String()))
	return items, true
}

func yamlScalarValue(value string) string {
	value = strings.TrimSpace(stripYAMLInlineComment(value))
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func stripYAMLInlineComment(value string) string {
	if comment := yamlInlineComment(value); comment != "" {
		return strings.TrimRightFunc(value[:len(value)-len(comment)], unicode.IsSpace)
	}
	return value
}

func yamlInlineComment(value string) string {
	var quote rune
	escaped := false
	for i, ch := range value {
		if quote != 0 {
			if quote == '"' && ch == '\\' && !escaped {
				escaped = true
				continue
			}
			if ch == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '#':
			if i == 0 || unicode.IsSpace(rune(value[i-1])) {
				return value[i:]
			}
		}
	}
	return ""
}
