package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/web"
)

func TestRegisterAll(t *testing.T) {
	reg := dtool.NewRegistry()

	if err := web.RegisterAll(reg); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	want := []string{"web.fetch", "web.request"}
	got := reg.Names()

	if len(got) != len(want) {
		t.Fatalf("registered %d tools, want %d: %v", len(got), len(want), got)
	}

	for i, name := range want {
		if got[i] != name {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer srv.Close()

	result, err := web.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if result.Status != http.StatusOK {
		t.Errorf("status = %d, want %d", result.Status, http.StatusOK)
	}

	if result.Body != "hello from server" {
		t.Errorf("body = %q, want %q", result.Body, "hello from server")
	}

	if result.Truncated {
		t.Error("expected truncated=false")
	}
}

func TestFetch_Truncation(t *testing.T) {
	bigBody := strings.Repeat("x", 1<<20+100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	result, err := web.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if !result.Truncated {
		t.Error("expected truncated=true for large response")
	}

	if len(result.Body) != 1<<20 {
		t.Errorf("body length = %d, want %d", len(result.Body), 1<<20)
	}
}

func TestRequest_POST(t *testing.T) {
	var gotMethod, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method

		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	result, err := web.Request(web.RequestArgs{
		Method:  "POST",
		URL:     srv.URL,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"key":"value"}`,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}

	if gotBody != `{"key":"value"}` {
		t.Errorf("body = %q, want %q", gotBody, `{"key":"value"}`)
	}

	if result.Status != http.StatusCreated {
		t.Errorf("status = %d, want %d", result.Status, http.StatusCreated)
	}
}

func TestRequest_EmptyURL(t *testing.T) {
	_, err := web.Request(web.RequestArgs{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}
