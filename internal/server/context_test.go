package server

import (
	"testing"

	"github.com/sousaakira/tend/internal/proto"
)

// TestTheContextBufferKeepsTheNewestAndSaysWhenItChanges: items get ids in
// order, the buffer keeps its newest contextMax, removing and clearing do
// what they say, and every change is announced. If it regresses, an open
// context panel shows a stale buffer, or the buffer grows without end.
func TestTheContextBufferKeepsTheNewestAndSaysWhenItChanges(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sub := s.Subscribe(contextMax + 16)
	defer sub.Close()

	if _, err := s.AddContext(proto.ContextItem{Kind: "text"}); err == nil {
		t.Error("an empty item is refused")
	}
	for i := 0; i < contextMax+2; i++ {
		if _, err := s.AddContext(proto.ContextItem{Kind: "text", Text: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	items := s.ContextItems()
	if len(items) != contextMax || items[0].ID != 3 || items[len(items)-1].ID != contextMax+2 || items[0].Created == 0 {
		t.Fatalf("the newest %d, in order: first %d last %d", contextMax, items[0].ID, items[len(items)-1].ID)
	}
	s.RemoveContext([]uint64{3, 4})
	if items := s.ContextItems(); len(items) != contextMax-2 || items[0].ID != 5 {
		t.Errorf("removed: %d items, first %d", len(items), items[0].ID)
	}
	s.RemoveContext(nil)
	if len(s.ContextItems()) != 0 {
		t.Error("cleared")
	}
	changes := 0
	for len(sub.C) > 0 {
		if ev := <-sub.C; ev.Kind == EventContextChanged {
			changes++
		}
	}
	if changes != contextMax+4 {
		t.Errorf("%d changes announced", changes)
	}
	if err := s.SendContext(1, nil); err == nil {
		t.Error("nothing to send")
	}
}
