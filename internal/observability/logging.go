// Package observability provides structured logging for symphony.
package observability

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured JSON logger for symphony.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// NewTextLogger creates a human-readable text logger for local development.
func NewTextLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}
