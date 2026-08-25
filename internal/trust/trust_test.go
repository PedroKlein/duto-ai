package trust_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/trust"
)

var testNow = time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC)

func TestDecode_NormalizesContexts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  map[string]any
		want trust.Context
	}{
		{name: "local", doc: localEvidence(), want: trust.ContextLocal},
		{name: "same repository", doc: githubEvidence("workflow_dispatch", "1001"), want: trust.ContextSameRepository},
		{name: "scheduled", doc: githubEvidence("schedule", "1001"), want: trust.ContextScheduled},
		{name: "forked pull request", doc: githubEvidence("pull_request", "9999"), want: trust.ContextForkedPR},
		{name: "unsupported event", doc: githubEvidence("workflow_run", "1001"), want: trust.ContextUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.doc)
			if err != nil {
				t.Fatal(err)
			}

			decision, err := trust.Decode(data, testNow)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			if decision.Context != tc.want {
				t.Fatalf("Context = %q, want %q", decision.Context, tc.want)
			}

			if !decision.Present || decision.ControlSHA256 == "" || decision.AdmissionID != "focused-m3" {
				t.Fatalf("Decision = %#v", decision)
			}
		})
	}
}

func TestDecode_RejectsStructurallyUnsafeJSON(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(localEvidence())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		data []byte
	}{
		{name: "duplicate", data: []byte(`{"version":1,"version":1}`)},
		{name: "explicit null", data: []byte(`{"version":1,"source":null}`)},
		{name: "unknown field", data: append(append([]byte{}, valid[:len(valid)-1]...), []byte(`,"unexpected":true}`)...)},
		{name: "trailing value", data: append(append([]byte{}, valid...), []byte(` {}`)...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := trust.Decode(tc.data, testNow); err == nil {
				t.Fatal("Decode() error = nil")
			}
		})
	}
}

func TestDecode_StaleEvidenceIsUnknown(t *testing.T) {
	t.Parallel()

	doc := localEvidence()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := trust.Decode(data, testNow.Add(7*time.Hour))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if decision.Context != trust.ContextUnknown {
		t.Fatalf("Context = %q, want unknown", decision.Context)
	}
}

func TestEligibilityFor_ExhaustiveMatrix(t *testing.T) {
	t.Parallel()

	contexts := []trust.Context{trust.ContextForkedPR, trust.ContextSameRepository, trust.ContextScheduled, trust.ContextLocal, trust.ContextUnknown}

	rows := map[trust.Capability][]trust.Eligibility{
		trust.CapabilityWorkspaceRead:   {trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly},
		trust.CapabilityWorkspaceMutate: {trust.EligibilityDenied, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityDenied},
		trust.CapabilityGitRead:         {trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly},
		trust.CapabilityGitMutate:       {trust.EligibilityDenied, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityDenied},
		trust.CapabilityGitPublish:      {trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged},
		trust.CapabilityProcessExec:     {trust.EligibilityDenied, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityDenied},
		trust.CapabilityNetworkRead:     {trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly},
		trust.CapabilityNetworkMutate:   {trust.EligibilityDenied, trust.EligibilityDenied, trust.EligibilityDenied, trust.EligibilityDenied, trust.EligibilityDenied},
		trust.CapabilityGitHubRead:      {trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly, trust.EligibilityReadOnly},
		trust.CapabilityGitHubMutate:    {trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged, trust.EligibilityStaged},
		trust.CapabilityAgentCall:       {trust.EligibilityReadOnly, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityGrant, trust.EligibilityReadOnly},
	}
	if len(rows) != 11 {
		t.Fatalf("capability rows = %d, want 11", len(rows))
	}

	for capability, expected := range rows {
		for i, context := range contexts {
			if got := trust.EligibilityFor(context, capability); got != expected[i] {
				t.Errorf("EligibilityFor(%q, %q) = %q, want %q", context, capability, got, expected[i])
			}
		}
	}
}

func localEvidence() map[string]any {
	return map[string]any{
		"version":    1,
		"source":     "local",
		"repository": map[string]any{"id": "local-repository", "owner_id": "local-owner", "owner": "example-owner", "name": "example-repository", "default_branch": "main"},
		"operator":   map[string]any{"profile": "developer"},
		"checkout":   map[string]any{"ref": "refs/heads/main", "sha": "1111111111111111111111111111111111111111"},
		"admission":  map[string]any{"id": "focused-m3", "correlation_key": "local-authoring", "issued_at": "2026-08-25T12:00:00Z", "expires_at": "2026-08-25T18:00:00Z"},
	}
}

func githubEvidence(eventName, headRepositoryID string) map[string]any {
	subject := map[string]any{"kind": "none", "number": 0}
	if eventName == "pull_request" {
		subject = map[string]any{"kind": "pull_request", "number": 42}
	}

	return map[string]any{
		"version":    1,
		"source":     "github",
		"repository": map[string]any{"id": "1001", "owner_id": "2001", "owner": "example-owner", "name": "example-repository", "default_branch": "main"},
		"event": map[string]any{
			"name": eventName, "actor_id": "3001", "subject": subject,
			"base": map[string]any{"repository_id": "1001", "ref": "refs/heads/main", "sha": "1111111111111111111111111111111111111111"},
			"head": map[string]any{"repository_id": headRepositoryID, "ref": "refs/heads/main", "sha": "1111111111111111111111111111111111111111"},
		},
		"run":       map[string]any{"id": "4001", "attempt": 1, "workflow_sha": "2222222222222222222222222222222222222222"},
		"checkout":  map[string]any{"ref": "refs/heads/main", "sha": "1111111111111111111111111111111111111111"},
		"admission": map[string]any{"id": "focused-m3", "correlation_key": "issue-42-authoring", "issued_at": "2026-08-25T12:00:00Z", "expires_at": "2026-08-25T18:00:00Z"},
	}
}
