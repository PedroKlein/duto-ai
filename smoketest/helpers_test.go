//go:build smoke

package smoketest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()

	val := os.Getenv(key)
	if val == "" {
		t.Skipf("skipping: %s not set", key)
	}

	return val
}

type recordedRequest struct {
	Method string
	Path   string
	Body   []byte
}

type mockGitHub struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

func setupMockGitHub(t *testing.T) *mockGitHub {
	t.Helper()

	mock := &mockGitHub{}

	mux := http.NewServeMux()

	// GET /repos/:owner/:repo/pulls/:number
	mux.HandleFunc("GET /repos/", func(w http.ResponseWriter, r *http.Request) {
		mock.record(r)

		// Check if it's a diff request
		if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write(loadFixture(t, "diff.patch"))

			return
		}

		// Check if it's a files request
		if contains(r.URL.Path, "/files") {
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "files.json"))

			return
		}

		// Default: PR metadata
		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "pr.json"))
	})

	// POST endpoints
	mux.HandleFunc("POST /repos/", func(w http.ResponseWriter, r *http.Request) {
		mock.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 1}`))
	})

	mock.server = httptest.NewServer(mux)

	t.Cleanup(func() { mock.server.Close() })

	return mock
}

func (m *mockGitHub) record(r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var body []byte
	if r.Body != nil {
		body, _ = readBody(r)
	}

	m.requests = append(m.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Body:   body,
	})
}

func (m *mockGitHub) getRequests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]recordedRequest, len(m.requests))
	copy(result, m.requests)

	return result
}

func (m *mockGitHub) hasGET() bool {
	for _, req := range m.getRequests() {
		if req.Method == "GET" {
			return true
		}
	}

	return false
}

func (m *mockGitHub) hasPOST() bool {
	for _, req := range m.getRequests() {
		if req.Method == "POST" {
			return true
		}
	}

	return false
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	defer r.Body.Close()

	buf := make([]byte, 0, 1024)
	for {
		tmp := make([]byte, 512)

		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)

		if err != nil {
			break
		}
	}

	return buf, nil
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/fixtures/" + name)
	if err != nil {
		t.Fatalf("loading fixture %s: %v", name, err)
	}

	return data
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)

	return data
}
