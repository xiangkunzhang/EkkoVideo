package store

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTripAndCoalesce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "state.json")
	s, initial := Open(p, []string{"default"})
	if len(initial) != 1 || initial[0] != "default" {
		t.Fatalf("got initial %v", initial)
	}

	s.Put([]string{"a"})
	s.Put([]string{"a", "b"})
	s.Put([]string{"final"})
	s.Close()

	s2, loaded := Open(p, []string(nil))
	defer s2.Close()
	if len(loaded) != 1 || loaded[0] != "final" {
		t.Fatalf("got loaded %v, want [final]", loaded)
	}
}
