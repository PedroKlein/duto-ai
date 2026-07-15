package logging_test

import (
	"log/slog"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/logging"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := logging.ParseLevel(tc.input)
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSetup_DoesNotPanic(_ *testing.T) {
	// Setup should not panic regardless of level.
	logging.Setup("debug")
	logging.Setup("info")
	logging.Setup("error")
	logging.Setup("invalid")
}
