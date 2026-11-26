// Package safego wraps background goroutines with panic recovery and structured logging.
package safego

import (
	"log"
	"runtime/debug"
)

// Go runs fn in a new goroutine and recovers any panic so a single background
// task or provider failure cannot crash the main application.
func Go(name string, logf func(string, ...any), fn func()) {
	if logf == nil {
		logf = log.Printf
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("panic in goroutine %q: %v\n%s", name, r, debug.Stack())
			}
		}()
		fn()
	}()
}
