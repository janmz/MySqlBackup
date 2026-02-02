// Package logger provides file-based append logging for mysqlbackup.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes lines to a file with optional stdout echo.
type Logger struct {
	f   *os.File
	mu  sync.Mutex
	echo bool
}

// New opens or creates the log file for appending. Creates parent dirs if needed.
func New(path string) (*Logger, error) {
	path = filepath.FromSlash(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f, echo: true}, nil
}

func (l *Logger) write(level, format string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format(time.RFC3339), level, fmt.Sprintf(format, a...))
	_, _ = l.f.WriteString(line)
	if l.echo {
		fmt.Print(line)
	}
}

// Info logs an info message.
func (l *Logger) Info(format string, a ...interface{}) { l.write("INFO", format, a...) }

// Warn logs a warning.
func (l *Logger) Warn(format string, a ...interface{}) { l.write("WARN", format, a...) }

// Error logs an error.
func (l *Logger) Error(format string, a ...interface{}) { l.write("ERROR", format, a...) }

// Close closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
