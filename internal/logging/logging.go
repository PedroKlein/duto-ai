// Package logging provides structured logging via slog with a global LevelVar.
package logging

import (
	"log/slog"
	"os"
)

// Level is the global log level variable, adjustable at runtime.
var Level = new(slog.LevelVar)

// Setup initializes the global slog logger with structured text output.
// Call this once at startup before any logging.
func Setup(level string) {
	Level.Set(ParseLevel(level))

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: Level,
	})

	slog.SetDefault(slog.New(handler))
}

// ParseLevel converts a string level name to slog.Level.
func ParseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
