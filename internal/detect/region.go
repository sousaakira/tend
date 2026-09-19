package detect

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Region names. A rule matches against one of these slices of the snapshot
// rather than the whole thing, which is what keeps a rule from firing on text
// that merely happens to be somewhere on screen.
const (
	RegionWholeRecent = "whole_recent"
	RegionOSCTitle    = "osc_title"
	RegionOSCProgress = "osc_progress"

	RegionAfterLastPromptMarker  = "after_last_prompt_marker"
	RegionBeforeCurrentPrompt    = "before_current_prompt_marker"
	RegionWholeWithoutCurrent    = "whole_recent_without_current_prompt_marker"
	RegionCurrentPromptBlock     = "current_prompt_block_marker"
	RegionAfterCurrentBlock      = "after_current_prompt_block_marker"
	RegionPromptBoxBody          = "prompt_box_body"
	RegionAbovePromptBox         = "above_prompt_box"
	RegionLastNonEmptyAboveBox   = "last_non_empty_above_prompt_box"
	RegionAfterLastHorizontalRow = "after_last_horizontal_rule"
)

// Parametrised region prefixes, written as name(count).
const (
	regionBottomLines         = "bottom_lines"
	regionBottomNonEmptyLines = "bottom_non_empty_lines"
	regionTopNonEmptyLines    = "top_non_empty_lines"
)

// topNonEmptyLinesEngineVersion is when top_non_empty_lines entered the rule
// vocabulary. A manifest using it must declare at least this version, or an
// older engine would silently evaluate the rule against the wrong text.
const topNonEmptyLinesEngineVersion = 3

// maxRegionLineCount bounds a region's line count so a malformed manifest
// cannot ask for an absurd slice.
const maxRegionLineCount = 65535

// validateRegion rejects a region a rule cannot mean, so a typo fails at load
// rather than silently matching against an empty string forever.
func validateRegion(spec string, minEngineVersion int) error {
	switch spec {
	case RegionWholeRecent, RegionOSCTitle, RegionOSCProgress,
		RegionAfterLastPromptMarker, RegionBeforeCurrentPrompt,
		RegionWholeWithoutCurrent, RegionCurrentPromptBlock,
		RegionAfterCurrentBlock, RegionPromptBoxBody,
		RegionAbovePromptBox, RegionLastNonEmptyAboveBox,
		RegionAfterLastHorizontalRow:
		return nil
	}
	if _, ok := regionCount(spec, regionBottomLines); ok {
		return nil
	}
	if _, ok := regionCount(spec, regionBottomNonEmptyLines); ok {
		return nil
	}
	if _, ok := regionCount(spec, regionTopNonEmptyLines); ok {
		if minEngineVersion < topNonEmptyLinesEngineVersion {
			return fmt.Errorf("region %q needs min_engine_version %d, manifest declares %d",
				spec, topNonEmptyLinesEngineVersion, minEngineVersion)
		}
		return nil
	}
	return fmt.Errorf("unknown region %q", spec)
}

// regionCount parses "name(123)".
func regionCount(spec, name string) (int, bool) {
	rest, ok := strings.CutPrefix(spec, name)
	if !ok {
		return 0, false
	}
	rest, ok = strings.CutPrefix(rest, "(")
	if !ok {
		return 0, false
	}
	rest, ok = strings.CutSuffix(rest, ")")
	if !ok {
		return 0, false
	}
	if rest == "" || strings.HasPrefix(rest, "0") {
		return 0, false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n > maxRegionLineCount {
		return 0, false
	}
	return n, true
}

// snapshot is a screen split into lines once, so that resolving a dozen rules
// against the same input does not re-split it a dozen times.
type snapshot struct {
	screen      string
	oscTitle    string
	oscProgress string

	lines  []string
	starts []int // byte offset at which each line begins
}

func newSnapshot(in Input) snapshot {
	s := snapshot{
		screen:      in.Screen,
		oscTitle:    in.OSCTitle,
		oscProgress: in.OSCProgress,
	}
	s.lines, s.starts = splitLines(in.Screen)
	return s
}

// splitLines matches Rust's str::lines, which the manifests were written
// against: a trailing newline terminates the last line rather than starting an
// empty one, and an empty string has no lines at all.
func splitLines(text string) ([]string, []int) {
	if text == "" {
		return nil, nil
	}
	n := strings.Count(text, "\n") + 1
	lines := make([]string, 0, n)
	starts := make([]int, 0, n)

	offset := 0
	rest := text
	for {
		i := strings.IndexByte(rest, '\n')
		if i < 0 {
			lines = append(lines, strings.TrimSuffix(rest, "\r"))
			starts = append(starts, offset)
			break
		}
		lines = append(lines, strings.TrimSuffix(rest[:i], "\r"))
		starts = append(starts, offset)
		offset += i + 1
		rest = rest[i+1:]
		if rest == "" {
			// The text ended with a newline; it terminated the previous line
			// rather than beginning an empty one.
			break
		}
	}
	return lines, starts
}

// lineStart returns the byte offset where line i begins, clamped to the end.
func (s *snapshot) lineStart(i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s.starts) {
		return len(s.screen)
	}
	return s.starts[i]
}

// from returns the screen text from line i onwards.
func (s *snapshot) from(i int) string {
	return s.screen[min(s.lineStart(i), len(s.screen)):]
}

// upto returns the screen text before line i.
func (s *snapshot) upto(i int) string {
	return s.screen[:min(s.lineStart(i), len(s.screen))]
}

// region resolves a region spec against the snapshot.
func (s *snapshot) region(spec string) string {
	switch spec {
	case RegionOSCTitle:
		return s.oscTitle
	case RegionOSCProgress:
		return s.oscProgress
	case RegionWholeRecent:
		return s.screen
	case RegionAfterLastPromptMarker:
		return s.afterLastPromptMarker()
	case RegionBeforeCurrentPrompt:
		return s.beforeCurrentPromptMarker()
	case RegionWholeWithoutCurrent:
		if _, ok := s.currentPromptIndex(); ok {
			return ""
		}
		return s.screen
	case RegionCurrentPromptBlock:
		return s.currentPromptBlockMarker()
	case RegionAfterCurrentBlock:
		return s.afterCurrentPromptBlockMarker()
	case RegionPromptBoxBody:
		return s.promptBoxBody()
	case RegionAbovePromptBox:
		return s.abovePromptBox()
	case RegionLastNonEmptyAboveBox:
		return lastNonEmptyLine(s.abovePromptBox())
	case RegionAfterLastHorizontalRow:
		return s.afterLastHorizontalRule()
	}

	if n, ok := regionCount(spec, regionBottomLines); ok {
		return s.bottomLines(n)
	}
	if n, ok := regionCount(spec, regionBottomNonEmptyLines); ok {
		return s.bottomNonEmptyLines(n)
	}
	if n, ok := regionCount(spec, regionTopNonEmptyLines); ok {
		return s.topNonEmptyLines(n)
	}
	// Unreachable for a validated manifest; an unknown region matches nothing
	// rather than matching everything.
	return ""
}

func (s *snapshot) bottomLines(count int) string {
	start := max(len(s.lines)-count, 0)
	return s.from(start)
}

// bottomNonEmptyLines returns the text from the count-th non-empty line
// counted from the bottom. Blank lines between them are included, because the
// shape of a block matters to the rules that read it.
func (s *snapshot) bottomNonEmptyLines(count int) string {
	seen := 0
	for i := len(s.lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(s.lines[i]) == "" {
			continue
		}
		seen++
		if seen == count {
			return s.from(i)
		}
	}
	if seen == 0 {
		return ""
	}
	// Fewer non-empty lines than asked for: everything from the first one.
	for i := 0; i < len(s.lines); i++ {
		if strings.TrimSpace(s.lines[i]) != "" {
			return s.from(i)
		}
	}
	return ""
}

func (s *snapshot) topNonEmptyLines(count int) string {
	seen := 0
	for i := 0; i < len(s.lines); i++ {
		if strings.TrimSpace(s.lines[i]) == "" {
			continue
		}
		seen++
		if seen == count {
			return s.upto(i + 1)
		}
	}
	if seen == 0 {
		return ""
	}
	for i := len(s.lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(s.lines[i]) != "" {
			return s.upto(i + 1)
		}
	}
	return ""
}

// --- prompt and block markers ----------------------------------------------

// isPromptLine recognises the prompt marker some agents draw at the point of
// input.
func isPromptLine(line string) bool {
	return line == "›" || strings.HasPrefix(line, "› ")
}

// isBlockMarkerLine recognises the bullets an agent uses to open a result
// block.
func isBlockMarkerLine(line string) bool {
	return strings.HasPrefix(line, "•") ||
		strings.HasPrefix(line, "■") ||
		strings.HasPrefix(line, "✗") ||
		strings.HasPrefix(line, "✓")
}

func (s *snapshot) lastPromptIndex() (int, bool) {
	for i := len(s.lines) - 1; i >= 0; i-- {
		if isPromptLine(s.lines[i]) {
			return i, true
		}
	}
	return 0, false
}

// currentPromptIndex finds the prompt only when it is still current: a block
// marker after it means the agent has already moved on and that prompt is
// history.
func (s *snapshot) currentPromptIndex() (int, bool) {
	i, ok := s.lastPromptIndex()
	if !ok {
		return 0, false
	}
	for j := i + 1; j < len(s.lines); j++ {
		if isBlockMarkerLine(s.lines[j]) {
			return 0, false
		}
	}
	return i, true
}

func (s *snapshot) afterLastPromptMarker() string {
	i, ok := s.lastPromptIndex()
	if !ok {
		return s.screen
	}
	return s.from(i + 1)
}

func (s *snapshot) beforeCurrentPromptMarker() string {
	i, ok := s.currentPromptIndex()
	if !ok {
		return s.screen
	}
	return s.upto(i)
}

func (s *snapshot) currentPromptBlockMarker() string {
	prompt, ok := s.currentPromptIndex()
	if !ok {
		return ""
	}
	for i := prompt - 1; i >= 0; i-- {
		if isBlockMarkerLine(s.lines[i]) {
			return s.lines[i]
		}
	}
	return ""
}

func (s *snapshot) afterCurrentPromptBlockMarker() string {
	prompt, ok := s.currentPromptIndex()
	if !ok {
		return ""
	}
	for i := prompt - 1; i >= 0; i-- {
		if isBlockMarkerLine(s.lines[i]) {
			return s.from(i)
		}
	}
	return ""
}

// --- horizontal rules and the prompt box -----------------------------------

// isHorizontalRule recognises a box border drawn with '─'. A short run counts
// only when nothing follows it, which keeps a line of prose that happens to
// start with a dash from reading as a border.
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	ruleChars := 0
	rest := trimmed
	for len(rest) > 0 {
		r, size := utf8.DecodeRuneInString(rest)
		if r != '\u2500' {
			break
		}
		ruleChars++
		rest = rest[size:]
	}
	if ruleChars == 0 {
		return false
	}
	return strings.TrimLeftFunc(rest, unicode.IsSpace) == "" || ruleChars >= 3
}

// promptBoxTopBorderIndex finds the second horizontal rule counting upwards:
// the prompt box is bounded by two, and the lower one is its bottom.
func (s *snapshot) promptBoxTopBorderIndex() (int, bool) {
	borders := 0
	for i := len(s.lines) - 1; i >= 0; i-- {
		if isHorizontalRule(s.lines[i]) {
			borders++
			if borders == 2 {
				return i, true
			}
		}
	}
	return 0, false
}

func (s *snapshot) promptBoxBody() string {
	top, ok := s.promptBoxTopBorderIndex()
	if !ok {
		return ""
	}
	end := len(s.lines)
	for i := top + 1; i < len(s.lines); i++ {
		if isHorizontalRule(s.lines[i]) {
			end = i
			break
		}
	}
	start := min(s.lineStart(top+1), len(s.screen))
	stop := min(s.lineStart(end), len(s.screen))
	if start > stop {
		return ""
	}
	return s.screen[start:stop]
}

func (s *snapshot) abovePromptBox() string {
	top, ok := s.promptBoxTopBorderIndex()
	if !ok {
		return s.screen
	}
	return s.upto(top)
}

func (s *snapshot) afterLastHorizontalRule() string {
	end := 0
	for i := 0; i < len(s.lines); i++ {
		if isHorizontalRule(s.lines[i]) {
			end = min(s.lineStart(i+1), len(s.screen))
		}
	}
	return s.screen[end:]
}

func lastNonEmptyLine(content string) string {
	lines, _ := splitLines(content)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}
