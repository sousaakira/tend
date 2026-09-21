// Package agentview is a filter and an order for the agent list that a script
// or a plugin puts in place: herdr's agent.view.set (`app/agent_view.rs`,
// `agent_view_eval.rs`). A plugin that knows which agents matter now — the
// ones in this space, the ones waiting, the ones on a given branch — can say
// so, and the list shows those, in its order, until it is cleared.
//
// It is pure: a view, entries that describe agents, and the answer. The
// server keeps the view, since it is a fact about the session every client
// shows; each client applies it to the list it draws.
package agentview

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Limits, herdr's.
const (
	maxDepth      = 8
	maxNodes      = 64
	maxValues     = 32
	maxSortFields = 8
	maxSource     = 120
	maxLabel      = 32
)

// View is what agent.view.set sets.
type View struct {
	Source string  `json:"source"`
	Label  string  `json:"label,omitempty"`
	Filter *Filter `json:"filter,omitempty"`
	Sort   []Sort  `json:"sort,omitempty"`
}

// Filter is one node of herdr's filter tree, tagged by op.
type Filter struct {
	Op      string   `json:"op"` // all, any, not, eq, in, exists
	Filters []Filter `json:"filters,omitempty"`
	Filter  *Filter  `json:"filter,omitempty"`
	Field   *Field   `json:"field,omitempty"`
	Value   *Value   `json:"value,omitempty"`
	Values  []Value  `json:"values,omitempty"`
}

// Field is a built-in field's name, or {"token": "name"} for a value a hook
// reported.
type Field struct {
	Builtin string
	Token   string
}

// UnmarshalJSON reads either form.
func (f *Field) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		f.Builtin = name
		return nil
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &token); err != nil || token.Token == "" {
		return errors.New("an agent view field is a name or {\"token\": \"name\"}")
	}
	f.Token = token.Token
	return nil
}

// MarshalJSON writes it back the way it came.
func (f Field) MarshalJSON() ([]byte, error) {
	if f.Token != "" {
		return json.Marshal(map[string]string{"token": f.Token})
	}
	return json.Marshal(f.Builtin)
}

// Value is a string, a boolean, a number, or {"context": ...} for what the
// client is looking at.
type Value struct {
	String  *string
	Bool    *bool
	Number  *uint64
	Context string
}

// UnmarshalJSON reads any of the forms.
func (v *Value) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		v.String = &s
		return nil
	}
	var b bool
	if json.Unmarshal(data, &b) == nil {
		v.Bool = &b
		return nil
	}
	var n uint64
	if json.Unmarshal(data, &n) == nil {
		v.Number = &n
		return nil
	}
	var c struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Context == "" {
		return errors.New("an agent view value is a string, a boolean, a number or {\"context\": ...}")
	}
	v.Context = c.Context
	return nil
}

// MarshalJSON writes it back.
func (v Value) MarshalJSON() ([]byte, error) {
	switch {
	case v.String != nil:
		return json.Marshal(*v.String)
	case v.Bool != nil:
		return json.Marshal(*v.Bool)
	case v.Number != nil:
		return json.Marshal(*v.Number)
	}
	return json.Marshal(map[string]string{"context": v.Context})
}

// Sort is one field to order by.
type Sort struct {
	Field Field  `json:"field"`
	Order string `json:"order,omitempty"` // asc (default) or desc
}

var (
	filterFields = map[string]bool{"status": true, "workspace_id": true, "tab_id": true,
		"pane_id": true, "agent": true, "seen": true, "state_change_seq": true}
	sortFields = map[string]bool{"workspace_order": true, "tab_order": true, "pane_order": true,
		"attention": true, "status": true, "agent": true, "seen": true, "state_change_seq": true}
	contexts = map[string]bool{"current_workspace_id": true, "current_tab_id": true}
)

// Validate checks a view against herdr's rules and limits, and tidies its
// source and label.
func (v *View) Validate() error {
	v.Source = strings.TrimSpace(v.Source)
	if err := ValidSource(v.Source); err != nil {
		return err
	}
	v.Label = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(v.Label))
	if len([]rune(v.Label)) > maxLabel {
		return fmt.Errorf("agent view label may be at most %d characters", maxLabel)
	}
	nodes := 0
	if v.Filter != nil {
		if err := v.Filter.validate(1, &nodes); err != nil {
			return err
		}
	}
	if len(v.Sort) > maxSortFields {
		return fmt.Errorf("agent view sort may contain at most %d fields", maxSortFields)
	}
	for _, s := range v.Sort {
		if s.Field.Token == "" && !sortFields[s.Field.Builtin] {
			return fmt.Errorf("agent view cannot sort by %q", s.Field.Builtin)
		}
		if s.Order != "" && s.Order != "asc" && s.Order != "desc" {
			return fmt.Errorf("agent view sort order is asc or desc, not %q", s.Order)
		}
	}
	return nil
}

// ValidSource is herdr's rule for who set a view.
func ValidSource(source string) error {
	ok := source != "" && len(source) <= maxSource
	for _, r := range source {
		ok = ok && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == ':' || r == '.' || r == '_' || r == '-')
	}
	if !ok {
		return fmt.Errorf("agent view source must be non-empty, at most %d characters, and contain only ASCII letters, digits, colon, dot, underscore, or hyphen", maxSource)
	}
	return nil
}

func (f *Filter) validate(depth int, nodes *int) error {
	*nodes++
	if depth > maxDepth {
		return fmt.Errorf("agent view filter may be at most %d deep", maxDepth)
	}
	if *nodes > maxNodes {
		return fmt.Errorf("agent view filter may contain at most %d nodes", maxNodes)
	}
	checkField := func() error {
		if f.Field == nil {
			return fmt.Errorf("agent view %s filter needs a field", f.Op)
		}
		if f.Field.Token == "" && !filterFields[f.Field.Builtin] {
			return fmt.Errorf("agent view cannot filter on %q", f.Field.Builtin)
		}
		return nil
	}
	checkValue := func(v Value) error {
		if v.Context != "" && !contexts[v.Context] {
			return fmt.Errorf("agent view has no context %q", v.Context)
		}
		return nil
	}
	switch f.Op {
	case "all", "any":
		for i := range f.Filters {
			if err := f.Filters[i].validate(depth+1, nodes); err != nil {
				return err
			}
		}
	case "not":
		if f.Filter == nil {
			return errors.New("agent view not filter needs a filter")
		}
		return f.Filter.validate(depth+1, nodes)
	case "eq":
		if err := checkField(); err != nil {
			return err
		}
		if f.Value == nil {
			return errors.New("agent view eq filter needs a value")
		}
		return checkValue(*f.Value)
	case "in":
		if err := checkField(); err != nil {
			return err
		}
		if len(f.Values) > maxValues {
			return fmt.Errorf("agent view in filter may list at most %d values", maxValues)
		}
		for _, v := range f.Values {
			if err := checkValue(v); err != nil {
				return err
			}
		}
	case "exists":
		return checkField()
	default:
		return fmt.Errorf("agent view filter has no op %q", f.Op)
	}
	return nil
}

// Entry describes one agent in the list.
type Entry struct {
	Status                     string // blocked, done, working, idle, unknown
	WorkspaceID, TabID, PaneID string
	Agent                      string
	Seen                       bool
	StateChangeSeq             uint64
	Tokens                     map[string]string
	WorkspaceOrder, TabOrder   uint64
	PaneOrder                  uint64
	Attention                  uint64
}

// Context is what the client is looking at, for {"context": ...} values.
type Context struct {
	WorkspaceID, TabID string
}

// value is one side of a comparison: absent, or a kind and what it holds.
type value struct {
	kind int // 1 string, 2 bool, 3 number
	s    string
	b    bool
	n    uint64
}

func (e Entry) field(f Field) (value, bool) {
	if f.Token != "" {
		v, ok := e.Tokens[f.Token]
		return value{kind: 1, s: v}, ok
	}
	switch f.Builtin {
	case "status":
		return value{kind: 1, s: e.Status}, true
	case "workspace_id":
		return value{kind: 1, s: e.WorkspaceID}, e.WorkspaceID != ""
	case "tab_id":
		return value{kind: 1, s: e.TabID}, e.TabID != ""
	case "pane_id":
		return value{kind: 1, s: e.PaneID}, e.PaneID != ""
	case "agent":
		return value{kind: 1, s: e.Agent}, e.Agent != ""
	case "seen":
		return value{kind: 2, b: e.Seen}, true
	case "state_change_seq":
		return value{kind: 3, n: e.StateChangeSeq}, e.StateChangeSeq != 0
	case "workspace_order":
		return value{kind: 3, n: e.WorkspaceOrder}, true
	case "tab_order":
		return value{kind: 3, n: e.TabOrder}, true
	case "pane_order":
		return value{kind: 3, n: e.PaneOrder}, true
	case "attention":
		return value{kind: 3, n: e.Attention}, true
	}
	return value{}, false
}

func operand(c Context, v Value) (value, bool) {
	switch {
	case v.String != nil:
		return value{kind: 1, s: *v.String}, true
	case v.Bool != nil:
		return value{kind: 2, b: *v.Bool}, true
	case v.Number != nil:
		return value{kind: 3, n: *v.Number}, true
	case v.Context == "current_workspace_id":
		return value{kind: 1, s: c.WorkspaceID}, c.WorkspaceID != ""
	case v.Context == "current_tab_id":
		return value{kind: 1, s: c.TabID}, c.TabID != ""
	}
	return value{}, false
}

// Matches reports whether an entry passes a filter.
func (f *Filter) Matches(c Context, e Entry) bool {
	switch f.Op {
	case "all":
		for i := range f.Filters {
			if !f.Filters[i].Matches(c, e) {
				return false
			}
		}
		return true
	case "any":
		for i := range f.Filters {
			if f.Filters[i].Matches(c, e) {
				return true
			}
		}
		return false
	case "not":
		return !f.Filter.Matches(c, e)
	case "eq":
		got, ok := e.field(*f.Field)
		want, wok := operand(c, *f.Value)
		return ok == wok && (!ok || got == want)
	case "in":
		got, ok := e.field(*f.Field)
		for _, v := range f.Values {
			want, wok := operand(c, v)
			if ok == wok && (!ok || got == want) {
				return true
			}
		}
		return false
	case "exists":
		_, ok := e.field(*f.Field)
		return ok
	}
	return false
}

// Less orders two entries by the view's sort: each field in turn, a
// missing value after a present one, as herdr compares them.
func (v *View) Less(a, b Entry) bool {
	for _, s := range v.Sort {
		av, aok := a.field(s.Field)
		bv, bok := b.field(s.Field)
		switch {
		case aok && !bok:
			return true
		case !aok && bok:
			return false
		case !aok && !bok:
			continue
		}
		c := compare(av, bv)
		if s.Order == "desc" {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
	}
	return false
}

func compare(a, b value) int {
	if a.kind != b.kind {
		return a.kind - b.kind
	}
	switch a.kind {
	case 1:
		return strings.Compare(a.s, b.s)
	case 2:
		switch {
		case a.b == b.b:
			return 0
		case !a.b:
			return -1
		}
		return 1
	}
	switch {
	case a.n < b.n:
		return -1
	case a.n > b.n:
		return 1
	}
	return 0
}
