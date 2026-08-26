package safeoutput_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/safeoutput"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func TestM3Staging_ReplyEnvelopeIsClosedAndDeterministic(t *testing.T) {
	t.Parallel()

	first := newCollector(t, safeoutput.ConversationReply, nil)
	second := newCollector(t, safeoutput.ConversationReply, nil)

	firstResult, err := first.Reply("run-example", safeoutput.ReplyArgs{Body: "Please provide the package path."})
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}

	secondResult, err := second.Reply("run-example", safeoutput.ReplyArgs{Body: "Please provide the package path."})
	if err != nil {
		t.Fatalf("Reply() second error = %v", err)
	}

	if firstResult.RequestID != secondResult.RequestID || len(firstResult.RequestID) != 64 {
		t.Fatalf("request IDs = %q and %q", firstResult.RequestID, secondResult.RequestID)
	}

	snapshot, err := first.Snapshot("run-example", true)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(snapshot.Operations) != 1 {
		t.Fatalf("operations = %d, want 1", len(snapshot.Operations))
	}

	encoded, err := json.Marshal(snapshot.Operations[0])
	if err != nil {
		t.Fatal(err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}

	want := []string{"version", "request_id", "correlation_key", "kind", "mode", "run_id", "plan_sha256", "policy_sha256", "control_sha256", "repository", "origin", "base", "source_commit", "depends_on", "preconditions", "payload"}
	if len(object) != len(want) {
		t.Fatalf("envelope fields = %v", object)
	}

	for _, name := range want {
		if _, exists := object[name]; !exists {
			t.Fatalf("envelope missing %q", name)
		}
	}

	if strings.Contains(string(encoded), "token") || strings.Contains(string(encoded), "endpoint") {
		t.Fatalf("envelope exposes authority: %s", encoded)
	}
}

func TestM3Staging_BranchPRDependencyAndCompletion(t *testing.T) {
	t.Parallel()

	collector := newCollector(t, safeoutput.BranchPR, func(context.Context) (safeoutput.Source, error) {
		return safeoutput.Source{Commit: strings.Repeat("3", 40), Tree: strings.Repeat("4", 40), Bundle: []byte("bundle")}, nil
	})
	if _, err := collector.DraftPR("run-example", safeoutput.DraftPRArgs{Title: "Title", Body: "Body"}); err == nil {
		t.Fatal("DraftPR() before Branch error = nil")
	}

	branch, err := collector.Branch(t.Context(), "run-example", safeoutput.BranchArgs{})
	if err != nil {
		t.Fatalf("Branch() error = %v", err)
	}

	pullRequest, err := collector.DraftPR("run-example", safeoutput.DraftPRArgs{Title: "Title", Body: "Body"})
	if err != nil {
		t.Fatalf("DraftPR() error = %v", err)
	}

	snapshot, err := collector.Snapshot("run-example", true)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(snapshot.Operations) != 2 || len(snapshot.Operations[1].DependsOn) != 1 || snapshot.Operations[1].DependsOn[0] != branch.RequestID {
		t.Fatalf("operation dependencies = %#v", snapshot.Operations)
	}

	if pullRequest.RequestID == branch.RequestID || string(snapshot.SourceBundle) != "bundle" {
		t.Fatalf("branch=%#v pullRequest=%#v bundle=%q", branch, pullRequest, snapshot.SourceBundle)
	}
}

func TestM3Staging_RejectsBoundsDuplicatesAndIncompleteSets(t *testing.T) {
	t.Parallel()

	reply := newCollector(t, safeoutput.ConversationReply, nil)
	if _, err := reply.Reply("run", safeoutput.ReplyArgs{Body: strings.Repeat("x", 129)}); err == nil {
		t.Fatal("oversized Reply() error = nil")
	}

	if _, err := reply.Snapshot("run", true); err == nil {
		t.Fatal("incomplete reply Snapshot() error = nil")
	}

	if _, err := reply.Reply("run", safeoutput.ReplyArgs{Body: "one"}); err != nil {
		t.Fatal(err)
	}

	if _, err := reply.Reply("run", safeoutput.ReplyArgs{Body: "two"}); err == nil {
		t.Fatal("duplicate Reply() error = nil")
	}

	branch := newCollector(t, safeoutput.BranchPR, func(context.Context) (safeoutput.Source, error) {
		return safeoutput.Source{Commit: strings.Repeat("3", 40), Tree: strings.Repeat("4", 40), Bundle: []byte("bundle")}, nil
	})
	if _, err := branch.Branch(t.Context(), "run", safeoutput.BranchArgs{}); err != nil {
		t.Fatal(err)
	}

	if _, err := branch.Snapshot("run", true); err == nil {
		t.Fatal("branch without draft PR Snapshot() error = nil")
	}
}

func TestM3Staging_FailedSnapshotCarriesOnlyRecovery(t *testing.T) {
	t.Parallel()

	collector := newCollector(t, safeoutput.None, nil)
	if err := collector.SetRecovery([]byte(`{"version":1}`+"\n"), []byte("patch")); err != nil {
		t.Fatalf("SetRecovery() error = %v", err)
	}

	snapshot, err := collector.Snapshot("run", false)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if snapshot.OperationSet != safeoutput.None || len(snapshot.Operations) != 0 || string(snapshot.RecoveryPatch) != "patch" {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
}

func TestM3Staging_ToolSchemasAreClosed(t *testing.T) {
	t.Parallel()

	collector := newCollector(t, safeoutput.ConversationReply, nil)

	registry := dtool.NewRegistry()
	if err := safeoutput.RegisterAll(registry, collector); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	value, exists := registry.Get("safe-output.conversation-reply")
	if !exists {
		t.Fatal("reply tool is not registered")
	}

	declared, ok := value.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	if !ok {
		t.Fatalf("tool type %T has no declaration", value)
	}

	encoded, err := json.Marshal(declared.Declaration().ParametersJsonSchema)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(encoded), `"additionalProperties":false`) {
		t.Fatalf("request schema is not closed: %s", encoded)
	}
}

func newCollector(t *testing.T, operationSet string, source safeoutput.SourceFunc) *safeoutput.Collector {
	t.Helper()

	limits := map[string]dtool.ToolLimit{}

	switch operationSet {
	case safeoutput.None:
	case safeoutput.ConversationReply:
		limits["safe-output.conversation-reply"] = dtool.ToolLimit{MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1024}
	case safeoutput.BranchPR:
		limits["safe-output.branch"] = dtool.ToolLimit{MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1024}
		limits["safe-output.draft-pr"] = dtool.ToolLimit{MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1024}
	}

	collector, err := safeoutput.New(safeoutput.Policy{
		OperationSet: operationSet, PlanSHA256: strings.Repeat("a", 64), PolicySHA256: strings.Repeat("b", 64),
		ControlSHA256: strings.Repeat("c", 64), ControlJSON: []byte(`{"version":1}`), CorrelationKey: "issue-42",
		Repository: safeoutput.Repository{ID: "1001", Owner: "example-owner", Name: "example-repository"},
		Origin:     safeoutput.Origin{Kind: "issue", Number: 42}, Base: safeoutput.Base{Ref: "refs/heads/main", SHA: strings.Repeat("1", 40)},
		BranchPrefix: "duto/m3/", MaxReplyBytes: 128, MaxPRTitleBytes: 64, MaxPRBodyBytes: 256,
		MaxBundleBytes: 1 << 20, Limits: limits, Source: source,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return collector
}
