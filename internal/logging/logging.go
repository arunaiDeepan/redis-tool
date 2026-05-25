// Package logging gives every redis-tool command a structured JSON log file in
// logs/<timestamp>-<cmd>.jsonl, alongside human-readable stderr output
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type Logger struct {
	*slog.Logger
	file *os.File
	path string
}

// New opens a fresh jsonl file under logsDir and returns a slog Logger that
// writes structured JSON to the file. Caller must Close() when done
func New(logsDir, command string, verbose bool) (*Logger, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(logsDir, fmt.Sprintf("%s-%s.jsonl", stamp, command))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	var writers []io.Writer = []io.Writer{f}
	mw := io.MultiWriter(writers...)

	handler := slog.NewJSONHandler(mw, &slog.HandlerOptions{Level: level})
	l := slog.New(handler).With("cmd", command)

	return &Logger{Logger: l, file: f, path: path}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	return l.file.Close()
}

func (l *Logger) Path() string { return l.path }

// logs an action lifecycle event with a uniform shape
func (l *Logger) Step(ctx context.Context, action, status string, attrs ...any) {
	all := append([]any{"action", action, "status", status}, attrs...)
	l.InfoContext(ctx, "step", all...)
}
