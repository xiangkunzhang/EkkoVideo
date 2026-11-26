package core

import (
	"testing"
	"time"
)

func TestQueueNavigation(t *testing.T) {
	items := []Item{
		{Ref: ItemRef{Source: "local", Kind: KindFile, ID: "/a.mp4"}, Title: "a"},
		{Ref: ItemRef{Source: "local", Kind: KindFile, ID: "/b.mp4"}, Title: "b"},
		{Ref: ItemRef{Source: "local", Kind: KindFile, ID: "/c.mp4"}, Title: "c"},
	}
	q := NewQueue(items, 1)
	if cur, ok := q.Active(); !ok || cur.Title != "b" {
		t.Fatalf("got %+v, ok=%v", cur, ok)
	}
	if nxt, ok := q.Next(); !ok || nxt.Title != "c" || q.HasNext() {
		t.Fatalf("next=%+v ok=%v hasNext=%v", nxt, ok, q.HasNext())
	}
	if prv, ok := q.Prev(); !ok || prv.Title != "b" {
		t.Fatalf("prev=%+v ok=%v", prv, ok)
	}
}

func TestHistoryResume(t *testing.T) {
	h := NewHistory(nil)
	ref := ItemRef{Source: "local", Kind: KindFile, ID: "/movie.mp4"}
	h.Record(WatchRecord{Ref: ref, Title: "Movie", Position: 120, Duration: 600})
	if got := h.ResumePosition(ref); got != 117*time.Second {
		t.Fatalf("resume = %v, want 117s", got)
	}
	h.Record(WatchRecord{Ref: ref, Title: "Movie", Position: 580, Duration: 600})
	if got := h.ResumePosition(ref); got != 0 {
		t.Fatalf("completed resume = %v, want 0", got)
	}
}

func TestSessionRecovery(t *testing.T) {
	s := NewPlaybackSession()
	gen := s.BeginResolve(nil)
	src := &PlaySource{
		URL: "http://line1",
		Lines: []Line{
			{ID: "1", URL: "http://line1"},
			{ID: "2", URL: "http://line2"},
		},
	}
	if !s.SetResolved(gen, src) {
		t.Fatal("SetResolved failed")
	}
	for i := 0; i < 2; i++ {
		if ok, idx := s.HandleFailure("timeout"); !ok || idx != 0 {
			t.Fatalf("retry %d: ok=%v idx=%d", i, ok, idx)
		}
	}
	if ok, idx := s.HandleFailure("timeout"); !ok || idx != 1 {
		t.Fatalf("expected switch to line 1, got ok=%v idx=%d", ok, idx)
	}
	s.HandleFailure("timeout")
	s.HandleFailure("timeout")
	if ok, _ := s.HandleFailure("timeout"); ok || s.Phase != PhaseFailed {
		t.Fatalf("expected PhaseFailed, got ok=%v phase=%v", ok, s.Phase)
	}
}
