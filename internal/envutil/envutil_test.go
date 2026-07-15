package envutil_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/envutil"
)

func TestExpand(t *testing.T) {
	t.Setenv("TEST_HOST", "localhost")
	t.Setenv("TEST_PORT", "8080")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single var",
			input: "${TEST_HOST}",
			want:  "localhost",
		},
		{
			name:  "multiple vars",
			input: "http://${TEST_HOST}:${TEST_PORT}/api",
			want:  "http://localhost:8080/api",
		},
		{
			name:  "missing var",
			input: "${NONEXISTENT_VAR}",
			want:  "",
		},
		{
			name:  "no vars",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := envutil.Expand(tc.input)
			if got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
