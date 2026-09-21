package server

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/sousaakira/tend/internal/plugin"
)

// A plugin's commands run where nobody sees them — a hook on every focus
// change, an action behind a key — so when one fails the only way to find
// out why is to be told afterwards. herdr keeps the last runs and answers
// plugin.log.list (`app/api/plugins/runtime.rs`); this is that, with its
// limit.

// pluginLogLimit is how many runs are kept, herdr's.
const pluginLogLimit = 200

// PluginLogEntry is one run of a plugin's command.
type PluginLogEntry struct {
	LogID      string   `json:"log_id"`
	PluginID   string   `json:"plugin_id"`
	ActionID   string   `json:"action_id,omitempty"`
	Event      string   `json:"event,omitempty"`
	Command    []string `json:"command"`
	Status     string   `json:"status"` // running, succeeded or failed
	StartedMs  int64    `json:"started_unix_ms"`
	FinishedMs int64    `json:"finished_unix_ms,omitempty"`
	ExitCode   *int     `json:"exit_code,omitempty"`
	Output     string   `json:"stdout,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type pluginLog struct {
	mu      sync.Mutex
	next    uint64
	entries []PluginLogEntry
}

// RunPlugin runs a plugin command and keeps a record of it. Every run a
// plugin makes goes through here, so none is missing from the log.
func (p *Plugins) RunPlugin(ctx context.Context, inv plugin.Invocation) plugin.Result {
	if p == nil {
		return plugin.Run(ctx, inv)
	}
	started := time.Now()
	p.log.mu.Lock()
	p.log.next++
	id := "log_" + strconv.FormatUint(p.log.next, 10)
	p.log.entries = append(p.log.entries, PluginLogEntry{
		LogID: id, PluginID: inv.Plugin.ID, ActionID: inv.ActionID, Event: inv.Event,
		Command: inv.Command, Status: "running", StartedMs: started.UnixMilli(),
	})
	if over := len(p.log.entries) - pluginLogLimit; over > 0 {
		p.log.entries = append([]PluginLogEntry(nil), p.log.entries[over:]...)
	}
	p.log.mu.Unlock()

	result := plugin.Run(ctx, inv)

	p.log.mu.Lock()
	for i := range p.log.entries {
		e := &p.log.entries[i]
		if e.LogID != id {
			continue
		}
		e.FinishedMs = time.Now().UnixMilli()
		code := result.ExitCode
		e.ExitCode = &code
		e.Output, e.Error = result.Output, result.Err
		e.Status = "succeeded"
		if result.Err != "" || result.ExitCode != 0 {
			e.Status = "failed"
		}
	}
	p.log.mu.Unlock()
	return result
}

// Log is the last runs, oldest first, for one plugin or all, at most limit.
func (p *Plugins) Log(pluginID string, limit int) []PluginLogEntry {
	if p == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > pluginLogLimit {
		limit = pluginLogLimit
	}
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	var out []PluginLogEntry
	for i := len(p.log.entries) - 1; i >= 0 && len(out) < limit; i-- {
		if pluginID == "" || p.log.entries[i].PluginID == pluginID {
			out = append(out, p.log.entries[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
