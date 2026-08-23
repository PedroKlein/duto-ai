package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/provider"
)

func TestNewLLM_UnknownType(t *testing.T) {
	cfg := config.Provider{
		Type:   "unknown",
		Config: map[string]string{},
	}

	_, err := provider.NewLLM(context.Background(), cfg, "model")
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
}

func TestBundledProvider_ADKv22Request(t *testing.T) {
	var authCalls atomic.Int32

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)

		if r.Method != http.MethodPost {
			t.Errorf("auth method = %q, want POST", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token",
			"expires_in":   3600,
			"token_type":   "bearer",
		}); err != nil {
			t.Errorf("encoding auth response: %v", err)
		}
	}))
	t.Cleanup(authServer.Close)

	var inferenceCalls atomic.Int32

	requestBodies := make(chan []byte, 1)
	inferenceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inferenceCalls.Add(1)

		if r.Method != http.MethodPost {
			t.Errorf("inference method = %q, want POST", r.Method)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q, want bearer token", got)
		}

		if !strings.HasSuffix(r.URL.Path, "/v2/completion") {
			t.Errorf("inference path = %q, want orchestration completion path", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading inference request: %v", err)
		}

		requestBodies <- body

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(map[string]any{
			"request_id": "request-1",
			"final_result": map[string]any{
				"id":    "completion-1",
				"model": "example-model",
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3,
				},
			},
		}); err != nil {
			t.Errorf("encoding inference response: %v", err)
		}
	}))
	t.Cleanup(inferenceServer.Close)

	cfg := config.Provider{
		Type: "ai-core",
		Config: map[string]string{
			"endpoint":       inferenceServer.URL,
			"resource_group": "example-group",
			"client_id":      "example-client",
			"client_secret":  "example-secret",
			"auth_url":       authServer.URL + "/token",
			"deployment_id":  "example-deployment",
		},
	}

	llm, err := provider.NewLLM(t.Context(), cfg, "example-model")
	if err != nil {
		t.Fatalf("provider.NewLLM: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:                 "inspect",
				ParametersJsonSchema: map[string]any{"type": "object"},
			}},
		}}},
	}

	var response *model.LLMResponse

	for item, err := range llm.GenerateContent(t.Context(), req, false) {
		if err != nil {
			t.Fatalf("GenerateContent: %v", err)
		}

		response = item
	}

	if response == nil || response.Content == nil || len(response.Content.Parts) == 0 {
		t.Fatal("bundled provider returned no content")
	}

	if got := response.Content.Parts[0].Text; got != "ok" {
		t.Fatalf("response text = %q, want ok", got)
	}

	if response.UsageMetadata == nil || response.UsageMetadata.PromptTokenCount != 2 || response.UsageMetadata.CandidatesTokenCount != 1 {
		t.Fatalf("usage = %#v, want reported prompt 2 and candidate 1", response.UsageMetadata)
	}

	body := <-requestBodies

	if !strings.Contains(string(body), `"name":"inspect"`) || !strings.Contains(string(body), `"parameters"`) {
		t.Fatalf("request omitted ParametersJsonSchema declaration: %s", body)
	}

	if got := authCalls.Load(); got != 1 {
		t.Fatalf("auth calls = %d, want 1", got)
	}

	if got := inferenceCalls.Load(); got != 1 {
		t.Fatalf("inference calls = %d, want 1", got)
	}
}
