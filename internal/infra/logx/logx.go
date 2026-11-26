// Package logx configures application logging to stderr and the platform log directory.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Logger writes timestamped lines to stderr and an optional log file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	log  *log.Logger
	path string
}

// Open creates or appends to the log file at path (if non-empty) and tees output to stderr.
func Open(path string) *Logger {
	var writers []io.Writer
	writers = append(writers, os.Stderr)

	var f *os.File
	if path != "" && path != "-" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if opened, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				f = opened
				writers = append(writers, f)
			}
		}
	}
	return &Logger{
		file: f,
		path: path,
		log:  log.New(io.MultiWriter(writers...), "", log.LstdFlags|log.Lmicroseconds),
	}
}

// Printf formats and writes a log line.
func (l *Logger) Printf(format string, args ...any) {
	if l == nil {
		log.Printf(format, args...)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Output(2, fmt.Sprintf(format, args...))
}

// Path returns the active log file path, if any.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close closes the underlying log file.
func (l *Logger) Close() {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.file.Close()
	l.file = nil
}
