// Package store provides non-blocking, atomic JSON persistence for main-thread owned state.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store persists values of type T to a JSON file without blocking the UI thread.
// Callers mutate state on the main thread and submit snapshots via Put; older
// queued snapshots are coalesced so only the latest state is written.
type Store[T any] struct {
	path    string
	save    chan T
	pending sync.WaitGroup
	once    sync.Once
}

// Open reads an existing JSON file into zero-value fallback on missing/corrupt
// files and starts the background writer goroutine.
func Open[T any](path string, fallback T) (*Store[T], T) {
	s := &Store[T]{
		path: path,
		save: make(chan T, 1),
	}
	val := fallback
	if data, err := os.ReadFile(path); err == nil {
		var parsed T
		if json.Unmarshal(data, &parsed) == nil {
			val = parsed
		}
	}
	go s.writer()
	return s, val
}

// Put queues a snapshot for background atomic write, dropping any unstarted older snapshot.
func (s *Store[T]) Put(snapshot T) {
	s.pending.Add(1)
	select {
	case <-s.save:
		s.pending.Done()
	default:
	}
	s.save <- snapshot
}

// Flush blocks until all queued writes have completed on disk.
func (s *Store[T]) Flush() {
	s.pending.Wait()
}

// Close flushes pending writes and stops the background writer goroutine.
func (s *Store[T]) Close() {
	s.once.Do(func() {
		s.Flush()
		close(s.save)
	})
}

func (s *Store[T]) writer() {
	for snapshot := range s.save {
		s.writeAtomic(snapshot)
		s.pending.Done()
	}
}

func (s *Store[T]) writeAtomic(snapshot T) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}
