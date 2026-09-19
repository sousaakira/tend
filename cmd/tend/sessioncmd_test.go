package main

import (
	"reflect"
	"testing"
)

func TestSplitCommands(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want [][]string
	}{
		{"empty", nil, nil},
		{"one command", []string{"claude"}, [][]string{{"claude"}}},
		{"leading separator", []string{"--", "claude"}, [][]string{{"claude"}}},
		{"two commands", []string{"claude", "--", "codex"}, [][]string{{"claude"}, {"codex"}}},
		{
			"commands with arguments",
			[]string{"sh", "-c", "date", "--", "htop", "-d", "5"},
			[][]string{{"sh", "-c", "date"}, {"htop", "-d", "5"}},
		},
		// A stray or doubled separator must not produce an empty command,
		// which would start a pane with nothing in it.
		{"doubled separator", []string{"a", "--", "--", "b"}, [][]string{{"a"}, {"b"}}},
		{"trailing separator", []string{"a", "--"}, [][]string{{"a"}}},
		{"only separators", []string{"--", "--"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitCommands(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("splitCommands(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
