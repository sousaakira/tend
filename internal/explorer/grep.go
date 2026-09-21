package explorer

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// The content search is git's: `git grep` knows which files are the
// project (tracked, and untracked but not ignored), skips binaries, and is
// fast. With --no-index it searches a directory git knows nothing about
// too, so there is one engine and not two that disagree.

// GrepOptions is what to search for and where.
type GrepOptions struct {
	Query string
	// Case makes the search case-sensitive; Word matches whole words only;
	// Regex reads the query as an extended regular expression rather than
	// as the text itself.
	Case, Word, Regex bool
	// Include and Exclude are comma-separated globs, as an editor's search
	// takes them: "*.go, docs/**" and "vendor/**".
	Include, Exclude string
}

// GrepMatch is one line that matched.
type GrepMatch struct {
	// Path is relative to the directory searched, with forward slashes.
	Path string
	Line int
	// Col is the byte offset of the first match on the line, from zero.
	Col  int
	Text string
}

// maxGrepMatches bounds a search: past it, the query is too loose to read
// the results of, and a project-wide `e` should not stall the panel.
const maxGrepMatches = 2000

// maxGrepLine bounds a line kept for showing; a minified file's one line is
// megabytes.
const maxGrepLine = 400

// grepArgs is the command line for a search in dir, in or out of git.
func grepArgs(o GrepOptions, inRepo bool) []string {
	args := []string{"grep", "-n", "--column", "-I", "--no-color", "--full-name"}
	if inRepo {
		// Untracked files are part of what an agent is working on; ignored
		// ones are not.
		args = append(args, "--untracked")
	} else {
		args = append(args, "--no-index", "--exclude-standard")
	}
	if !o.Case {
		args = append(args, "-i")
	}
	if o.Word {
		args = append(args, "-w")
	}
	if o.Regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	args = append(args, "-e", o.Query, "--")
	specs := pathspecs(o.Include, "")
	if len(specs) == 0 {
		specs = []string{"."}
	}
	args = append(args, specs...)
	args = append(args, pathspecs(o.Exclude, "exclude,")...)
	return args
}

// pathspecs turns "*.go, docs/**" into git's glob pathspecs. A bare pattern
// with no slash matches at any depth, as an editor's include box does.
func pathspecs(list, magic string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			p = "**/" + p
		}
		out = append(out, ":("+magic+"glob)"+p)
	}
	return out
}

// Grep searches dir. It stops at maxGrepMatches, or when ctx ends — a new
// keystroke makes the last search's answer worthless. truncated says there
// were more.
func Grep(ctx context.Context, dir string, inRepo bool, o GrepOptions) (matches []GrepMatch, truncated bool, err error) {
	if strings.TrimSpace(o.Query) == "" {
		return nil, false, nil
	}
	cmd := exec.CommandContext(ctx, "git", grepArgs(o, inRepo)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		m, ok := parseGrepLine(sc.Text())
		if !ok {
			continue
		}
		if len(matches) == maxGrepMatches {
			truncated = true
			break
		}
		matches = append(matches, m)
	}
	if truncated {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return nil, false, ctx.Err()
	case truncated:
		return matches, true, nil
	}
	if waitErr != nil {
		// Exit status 1 is "nothing matched", which is an answer.
		if ee, ok := waitErr.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return matches, false, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, false, errorf(strings.SplitN(msg, "\n", 2)[0])
		}
		return nil, false, waitErr
	}
	return matches, false, nil
}

type grepError string

func (e grepError) Error() string { return string(e) }

func errorf(msg string) error {
	// git's words for a bad pattern are long and start with "fatal: ".
	return grepError(strings.TrimPrefix(msg, "fatal: "))
}

// parseGrepLine reads "path:line:column:text". A path with a colon in it is
// read wrongly by this, which git's -z would fix at the cost of a harder
// format; such paths are rare enough in projects to accept.
func parseGrepLine(s string) (GrepMatch, bool) {
	parts := strings.SplitN(s, ":", 4)
	if len(parts) != 4 {
		return GrepMatch{}, false
	}
	line, err1 := strconv.Atoi(parts[1])
	col, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return GrepMatch{}, false
	}
	text := parts[3]
	if len(text) > maxGrepLine {
		text = text[:maxGrepLine]
	}
	return GrepMatch{Path: parts[0], Line: line, Col: col - 1, Text: text}, true
}
