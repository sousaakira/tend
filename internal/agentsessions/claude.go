// Package agentsessions reads the conversations agents keep on disk, to list,
// find and delete them. It is tend's own: herdr resumes the conversation a
// pane was in (agent_resume.rs, internal/agent here) and has no list of the
// rest.
//
// Only Claude Code's are read so far. It keeps each conversation as one JSON
// line file, <config>/projects/<the directory, / and . as ->/<id>.jsonl, with
// a folder of the same name beside it for what its subagents wrote. The
// format is Claude Code's and undocumented, so everything here reads it
// loosely: a line that does not parse is skipped, a field that is missing is
// left empty, and nothing is ever written back.
package agentsessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is one conversation.
type Session struct {
	Agent string
	ID    string
	// Title is the name given to it (/rename), else the one Claude Code
	// made up for it, else the first thing typed in it.
	Title string
	// Dir is the directory it was held in.
	Dir      string
	Modified time.Time
	// Size is the bytes it takes, the conversation and its subagents'.
	Size int64
	// Prompts is how many times the user wrote in it.
	Prompts int
	path    string
}

// ErrNoSuchSession is a delete of a conversation that is not there.
var ErrNoSuchSession = errors.New("no such session")

// idPattern is what Claude Code names a conversation: a UUID. A delete takes
// nothing else, so no name can reach outside the folder it is in.
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Claude reads the conversations under a Claude Code config directory.
// What it read is kept by each file's size and time, so listing again reads
// only what changed: the files run to megabytes, and the one being written
// to changes every few seconds while the others do not.
type Claude struct {
	root string

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	size    int64
	mod     time.Time
	session Session
}

// NewClaude reads the conversations under dir, Claude Code's config
// directory (~/.claude, or $CLAUDE_CONFIG_DIR).
func NewClaude(dir string) *Claude {
	return &Claude{root: filepath.Join(dir, "projects"), cache: map[string]cached{}}
}

// List is every conversation, the one written to last first. A machine with
// no Claude Code has none, which is not an error.
func (c *Claude) List() ([]Session, error) {
	files, err := filepath.Glob(filepath.Join(c.root, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]bool, len(files))
	out := make([]Session, 0, len(files))
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if !idPattern.MatchString(id) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue // deleted since the glob
		}
		seen[path] = true
		if hit, ok := c.cache[path]; ok && hit.size == info.Size() && hit.mod.Equal(info.ModTime()) {
			out = append(out, hit.session)
			continue
		}
		s, err := readClaude(path)
		if err != nil {
			continue
		}
		s.ID, s.Modified = id, info.ModTime()
		s.Size = info.Size() + dirSize(strings.TrimSuffix(path, ".jsonl"))
		c.cache[path] = cached{size: info.Size(), mod: info.ModTime(), session: s}
		out = append(out, s)
	}
	for path := range c.cache {
		if !seen[path] {
			delete(c.cache, path)
		}
	}
	// A conversation that never said where it was — one Claude Code only
	// named, and left — is where the others in its folder were: the
	// folder's name is the path with / and . both turned to -, which cannot
	// be turned back.
	known := map[string]string{}
	for _, s := range out {
		if s.Dir != "" {
			known[filepath.Dir(s.path)] = s.Dir
		}
	}
	for i := range out {
		if out[i].Dir == "" {
			if dir, ok := known[filepath.Dir(out[i].path)]; ok {
				out[i].Dir = dir
			} else {
				out[i].Dir = guessDir(filepath.Base(filepath.Dir(out[i].path)))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// Delete removes a conversation: its file and its subagents' folder. The
// folder of the project it was in is left, with Claude Code's memory in it.
func (c *Claude) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrNoSuchSession, id)
	}
	files, err := filepath.Glob(filepath.Join(c.root, "*", id+".jsonl"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchSession, id)
	}
	for _, path := range files {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.RemoveAll(strings.TrimSuffix(path, ".jsonl")); err != nil {
			return err
		}
		c.mu.Lock()
		delete(c.cache, path)
		c.mu.Unlock()
	}
	return nil
}

// line is the part of a line of the file that is read.
type line struct {
	Type      string          `json:"type"`
	Cwd       string          `json:"cwd"`
	AITitle   string          `json:"aiTitle"`
	AgentName string          `json:"agentName"`
	Message   json.RawMessage `json:"message"`
	Sidechain bool            `json:"isSidechain"`
	Origin    struct {
		Kind string `json:"kind"`
	} `json:"origin"`
}

// readClaude reads what the list shows from one file. Only the lines that
// can say something are decoded: the others — tool output, most of the
// bytes — are passed over by a look at their type first.
func readClaude(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	s := Session{Agent: "claude", path: path}
	var named, titled, first string
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		raw, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than the buffer is tool output or a pasted
			// image, never a title: skip to its end.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = r.ReadSlice('\n')
			}
			continue
		}
		if len(raw) > 0 {
			switch {
			case bytes.Contains(raw, []byte(`"type":"agent-name"`)),
				bytes.Contains(raw, []byte(`"type":"ai-title"`)),
				bytes.Contains(raw, []byte(`"type":"user"`)),
				s.Dir == "" && bytes.Contains(raw, []byte(`"cwd":`)):
				var l line
				if json.Unmarshal(raw, &l) == nil {
					if s.Dir == "" && l.Cwd != "" {
						s.Dir = l.Cwd
					}
					switch l.Type {
					case "agent-name":
						if l.AgentName != "" {
							named = l.AgentName
						}
					case "ai-title":
						if l.AITitle != "" {
							titled = l.AITitle
						}
					case "user":
						if text, ok := typed(l); ok {
							s.Prompts++
							if first == "" {
								first = text
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	switch {
	case named != "":
		s.Title = named
	case titled != "":
		s.Title = titled
	default:
		s.Title = first
	}
	s.Title = oneLine(s.Title)
	return s, nil
}

// typed is what the user wrote, from a user line that is theirs: not a
// subagent's, not a tool's result, not the output of a command Claude Code
// ran for them, which it also records as the user's.
func typed(l line) (string, bool) {
	if l.Sidechain || (l.Origin.Kind != "" && l.Origin.Kind != "human") {
		return "", false
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(l.Message, &msg) != nil {
		return "", false
	}
	var text string
	if json.Unmarshal(msg.Content, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(msg.Content, &blocks) != nil {
			return "", false
		}
		for _, b := range blocks {
			if b.Type == "text" {
				text = b.Text
				break
			}
		}
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "<") {
		return "", false
	}
	return text, true
}

// oneLine is text on one line and not past a list's width.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return s
}

// guessDir is a project folder's name back as a path, for a conversation
// that never said where it was: the name is the path with / and . as -, so
// this is right only when the path had neither in its names.
func guessDir(name string) string {
	return strings.ReplaceAll(name, "-", "/")
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
