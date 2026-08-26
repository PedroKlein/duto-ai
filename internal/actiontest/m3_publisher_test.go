package actiontest_test

// TestM3Publisher_* covers ADR 011 T5.1: publisher CLI, bundle/control/policy digest
// verification, disposition stability, forbidden effects, verify-before-credential, and
// within-activation idempotency. Tests are RED until P5 GREEN implements duto-ai publish.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

const (
	pubRepoID    = "1001"
	pubOwnerID   = "2001"
	pubOwner     = "example-owner"
	pubRepo      = "example-repository"
	pubCorrelKey = "issue-42-authoring"
	pubAdmID     = "focused-m3"
	pubBaseSHA   = "aaaa111111111111111111111111111111111111"
	pubSrcSHA    = "bbbb222222222222222222222222222222222222"
	pubRunID     = "5001"
	pubWorkflow  = "2222222222222222222222222222222222222222"
)

func pubHex(char string, n int) string { return strings.Repeat(char, n) }

func pubSHA(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }

func pubPolicyDigest(t *testing.T) string {
	t.Helper()

	cfg, err := config.DecodeConfig("duto.yaml", []byte(pubMinimalConfig(t)))
	if err != nil {
		t.Fatalf("publisher setup: decode config: %v", err)
	}

	digest, err := plan.M3PolicySHA256(cfg)
	if err != nil {
		t.Fatalf("publisher setup: policy digest: %v", err)
	}

	return digest
}

func pubEvidence(t *testing.T) map[string]any {
	t.Helper()

	now := time.Now().UTC()

	return map[string]any{
		"version": 1, "source": "github",
		"repository": map[string]any{"id": pubRepoID, "owner_id": pubOwnerID, "owner": pubOwner, "name": pubRepo, "default_branch": "main"},
		"event": map[string]any{
			"name": "issues", "actor_id": "3001",
			"subject": map[string]any{"kind": "issue", "number": 42},
			"base":    map[string]any{"repository_id": pubRepoID, "ref": "refs/heads/main", "sha": pubBaseSHA},
			"head":    map[string]any{"repository_id": pubRepoID, "ref": "refs/heads/main", "sha": pubBaseSHA},
		},
		"run":      map[string]any{"id": pubRunID, "attempt": 1, "workflow_sha": pubWorkflow},
		"checkout": map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"admission": map[string]any{
			"id": pubAdmID, "correlation_key": pubCorrelKey,
			"issued_at":  now.Add(-time.Minute).Format(time.RFC3339),
			"expires_at": now.Add(5 * time.Hour).Format(time.RFC3339),
		},
	}
}

func pubDispatchEvidence(t *testing.T) map[string]any {
	t.Helper()

	now := time.Now().UTC()

	return map[string]any{
		"version": 1, "source": "github",
		"repository": map[string]any{"id": pubRepoID, "owner_id": pubOwnerID, "owner": pubOwner, "name": pubRepo, "default_branch": "main"},
		"event": map[string]any{
			"name": "workflow_dispatch", "actor_id": "3001",
			"subject": map[string]any{"kind": "none", "number": 0},
			"base":    map[string]any{"repository_id": pubRepoID, "ref": "refs/heads/main", "sha": pubBaseSHA},
			"head":    map[string]any{"repository_id": pubRepoID, "ref": "refs/heads/main", "sha": pubBaseSHA},
		},
		"run":      map[string]any{"id": pubRunID, "attempt": 1, "workflow_sha": pubWorkflow},
		"checkout": map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"admission": map[string]any{
			"id": pubAdmID, "correlation_key": pubCorrelKey,
			"issued_at":  now.Add(-time.Minute).Format(time.RFC3339),
			"expires_at": now.Add(5 * time.Hour).Format(time.RFC3339),
		},
	}
}

func writePubEvidenceFile(t *testing.T, doc map[string]any) string {
	t.Helper()

	data, _ := json.Marshal(doc)

	f, err := os.CreateTemp(t.TempDir(), "pub-evidence-*.json")
	if err != nil {
		t.Fatalf("publisher setup: %v", err)
	}

	_, _ = f.Write(data)
	_ = f.Close()

	return f.Name()
}

func writePubRaw(t *testing.T, dir, name string, data []byte) {
	t.Helper()

	_ = os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("publisher setup: write %s: %v", name, err)
	}
}

func pubMinimalConfig(t *testing.T) string {
	t.Helper()

	return fmt.Sprintf(`version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  capable: {provider: default, target: example-model}
workspaces:
  source: {root: %s, access: write}
tools:
  - files.read
  - safe-output.conversation-reply
  - safe-output.branch
  - safe-output.draft-pr
tool_limits:
  files.read:                      {max_calls: 20, timeout: 15s, max_request_bytes: 4096,  max_result_bytes: 262144}
  safe-output.conversation-reply:  {max_calls: 1,  timeout: 5s,  max_request_bytes: 33792, max_result_bytes: 4096}
  safe-output.branch:              {max_calls: 1,  timeout: 5s,  max_request_bytes: 1024,  max_result_bytes: 4096}
  safe-output.draft-pr:            {max_calls: 1,  timeout: 5s,  max_request_bytes: 67584, max_result_bytes: 4096}
m3:
  admission:
    id: focused-m3
    contexts: [same_repository, scheduled, local]
    capabilities: [workspace.mutate, git.mutate, git.publish, github.mutate]
  authoring:
    workspace: source
    allowed_paths: [docs/]
    max_changed_files: 2
    max_file_bytes: 1024
    max_total_write_bytes: 2048
    max_commit_message_bytes: 256
    commit_author_name: Example Automation
    commit_author_email: automation@example.invalid
  publication:
    mode: staged
    operation_sets: [conversation-reply, branch-pr]
    branch_prefix: duto/m3/
    max_reply_bytes: 32768
    max_pr_title_bytes: 256
    max_pr_body_bytes: 65536
    max_bundle_bytes: 16777216
evidence:
  directory: %s
`, t.TempDir(), t.TempDir())
}

func writePubConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "duto.yaml")
	if err := os.WriteFile(path, []byte(pubMinimalConfig(t)), 0o644); err != nil {
		t.Fatalf("publisher setup: write config: %v", err)
	}

	return path
}

// buildReplyBundle returns (bundleDir, bundleSHA256) for a valid reply bundle.
func buildReplyBundle(t *testing.T, evidenceDoc map[string]any, body string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	controlJSON, _ := json.Marshal(evidenceDoc)
	writePubRaw(t, dir, "control.json", controlJSON)
	controlDigest := pubSHA(controlJSON)
	planDigest, policyDigest := pubHex("b", 64), pubPolicyDigest(t)
	payloadBytes, _ := json.Marshal(map[string]any{"body": body})
	payloadDigest := pubSHA(payloadBytes)
	ridHash := sha256.New()
	ridHash.Write([]byte(pubRunID + "\x00" + "conversation.reply" + "\x00" + payloadDigest))
	requestID := hex.EncodeToString(ridHash.Sum(nil))
	replyOp := map[string]any{
		"version": 1, "request_id": requestID, "correlation_key": pubCorrelKey,
		"kind": "conversation.reply", "mode": "staged", "run_id": pubRunID,
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository":    map[string]any{"id": pubRepoID, "owner": pubOwner, "name": pubRepo},
		"origin":        map[string]any{"kind": "issue", "number": 42},
		"base":          map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"source_commit": pubBaseSHA, "depends_on": []any{},
		"preconditions": map[string]any{"subject_state": "open"},
		"payload":       map[string]any{"body": body},
	}
	opData, _ := json.Marshal(replyOp)
	writePubRaw(t, dir, "operations/0001-conversation-reply.json", opData)

	eventsData, resultData, summaryData := []byte("{}\n"), []byte("{}\n"), []byte("Done.\n")
	writePubRaw(t, dir, "events.jsonl", eventsData)
	writePubRaw(t, dir, "result.json", resultData)
	writePubRaw(t, dir, "summary.md", summaryData)
	files := []map[string]any{
		{"name": "control.json", "size": len(controlJSON), "sha256": controlDigest},
		{"name": "events.jsonl", "size": len(eventsData), "sha256": pubSHA(eventsData)},
		{"name": "operations/0001-conversation-reply.json", "size": len(opData), "sha256": pubSHA(opData)},
		{"name": "result.json", "size": len(resultData), "sha256": pubSHA(resultData)},
		{"name": "summary.md", "size": len(summaryData), "sha256": pubSHA(summaryData)},
	}
	manifest := map[string]any{
		"version": 2, "bundle_kind": "m3-authoring", "run_id": pubRunID,
		"completion": "succeeded", "operation_set": "conversation-reply",
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository_id": pubRepoID, "base_ref": "refs/heads/main", "base_sha": pubBaseSHA,
		"source_commit": pubBaseSHA, "files": files,
	}
	manifestData, _ := json.Marshal(manifest)
	manifestData = append(manifestData, '\n')
	writePubRaw(t, dir, "manifest.json", manifestData)

	return dir, pubSHA(manifestData)
}

// buildBranchPRBundleNoAuthoredBundle builds a manifest listing authored.bundle but omits it.
func buildBranchPRBundleNoAuthoredBundle(t *testing.T, evidenceDoc map[string]any, targetRef string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	controlJSON, _ := json.Marshal(evidenceDoc)
	writePubRaw(t, dir, "control.json", controlJSON)
	controlDigest := pubSHA(controlJSON)
	planDigest, policyDigest := pubHex("b", 64), pubPolicyDigest(t)
	branchRID, prRID := pubHex("e", 64), pubHex("f", 64)
	branchOp := map[string]any{
		"version": 1, "request_id": branchRID, "correlation_key": pubCorrelKey,
		"kind": "git.branch.publish", "mode": "staged", "run_id": pubRunID,
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository":    map[string]any{"id": pubRepoID, "owner": pubOwner, "name": pubRepo},
		"origin":        map[string]any{"kind": "none", "number": 0},
		"base":          map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"source_commit": pubSrcSHA, "depends_on": []any{},
		"preconditions": map[string]any{"target_ref": targetRef, "target_state": "absent"},
		"payload":       map[string]any{},
	}
	prOp := map[string]any{
		"version": 1, "request_id": prRID, "correlation_key": pubCorrelKey,
		"kind": "pull_request.create_draft", "mode": "staged", "run_id": pubRunID,
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository":    map[string]any{"id": pubRepoID, "owner": pubOwner, "name": pubRepo},
		"origin":        map[string]any{"kind": "none", "number": 0},
		"base":          map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"source_commit": pubSrcSHA, "depends_on": []any{branchRID},
		"preconditions": map[string]any{"head_ref": targetRef, "pull_request_state": "absent", "draft": true},
		"payload":       map[string]any{"title": "Update", "body": "Summary."},
	}
	branchData, _ := json.Marshal(branchOp)
	prData, _ := json.Marshal(prOp)

	writePubRaw(t, dir, "operations/0001-branch.json", branchData)
	writePubRaw(t, dir, "operations/0002-draft-pr.json", prData)

	eventsData, resultData, summaryData := []byte("{}\n"), []byte("{}\n"), []byte("Done.\n")
	writePubRaw(t, dir, "events.jsonl", eventsData)
	writePubRaw(t, dir, "result.json", resultData)
	writePubRaw(t, dir, "summary.md", summaryData)
	// authored.bundle listed but NOT written — triggers structural rejection.
	fakeSize, fakeSHA := 512, pubHex("5", 64)
	files := []map[string]any{
		{"name": "authored.bundle", "size": fakeSize, "sha256": fakeSHA},
		{"name": "control.json", "size": len(controlJSON), "sha256": controlDigest},
		{"name": "events.jsonl", "size": len(eventsData), "sha256": pubSHA(eventsData)},
		{"name": "operations/0001-branch.json", "size": len(branchData), "sha256": pubSHA(branchData)},
		{"name": "operations/0002-draft-pr.json", "size": len(prData), "sha256": pubSHA(prData)},
		{"name": "result.json", "size": len(resultData), "sha256": pubSHA(resultData)},
		{"name": "summary.md", "size": len(summaryData), "sha256": pubSHA(summaryData)},
	}
	manifest := map[string]any{
		"version": 2, "bundle_kind": "m3-authoring", "run_id": pubRunID,
		"completion": "succeeded", "operation_set": "branch-pr",
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository_id": pubRepoID, "base_ref": "refs/heads/main", "base_sha": pubBaseSHA,
		"source_commit": pubSrcSHA, "files": files,
	}
	manifestData, _ := json.Marshal(manifest)
	manifestData = append(manifestData, '\n')
	writePubRaw(t, dir, "manifest.json", manifestData)

	return dir, pubSHA(manifestData)
}

// buildPROnlyBundle builds a bundle with only a draft-pr op (no branch dependency).
func buildPROnlyBundle(t *testing.T, evidenceDoc map[string]any) (string, string) {
	t.Helper()

	dir := t.TempDir()
	controlJSON, _ := json.Marshal(evidenceDoc)
	writePubRaw(t, dir, "control.json", controlJSON)
	controlDigest := pubSHA(controlJSON)
	planDigest, policyDigest := pubHex("b", 64), pubPolicyDigest(t)
	prRID := pubHex("f", 64)
	prOp := map[string]any{
		"version": 1, "request_id": prRID, "correlation_key": pubCorrelKey,
		"kind": "pull_request.create_draft", "mode": "staged", "run_id": pubRunID,
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository":    map[string]any{"id": pubRepoID, "owner": pubOwner, "name": pubRepo},
		"origin":        map[string]any{"kind": "none", "number": 0},
		"base":          map[string]any{"ref": "refs/heads/main", "sha": pubBaseSHA},
		"source_commit": pubSrcSHA, "depends_on": []any{},
		"preconditions": map[string]any{"head_ref": "refs/heads/duto/m3/" + pubCorrelKey, "pull_request_state": "absent", "draft": true},
		"payload":       map[string]any{"title": "Update", "body": "Summary."},
	}
	prData, _ := json.Marshal(prOp)
	writePubRaw(t, dir, "operations/0001-draft-pr.json", prData)

	eventsData, resultData, summaryData := []byte("{}\n"), []byte("{}\n"), []byte("Done.\n")
	writePubRaw(t, dir, "events.jsonl", eventsData)
	writePubRaw(t, dir, "result.json", resultData)
	writePubRaw(t, dir, "summary.md", summaryData)

	fakeSize, fakeSHA := 512, pubHex("5", 64)
	files := []map[string]any{
		{"name": "authored.bundle", "size": fakeSize, "sha256": fakeSHA},
		{"name": "control.json", "size": len(controlJSON), "sha256": controlDigest},
		{"name": "events.jsonl", "size": len(eventsData), "sha256": pubSHA(eventsData)},
		{"name": "operations/0001-draft-pr.json", "size": len(prData), "sha256": pubSHA(prData)},
		{"name": "result.json", "size": len(resultData), "sha256": pubSHA(resultData)},
		{"name": "summary.md", "size": len(summaryData), "sha256": pubSHA(summaryData)},
	}
	manifest := map[string]any{
		"version": 2, "bundle_kind": "m3-authoring", "run_id": pubRunID,
		"completion": "succeeded", "operation_set": "branch-pr",
		"plan_sha256": planDigest, "policy_sha256": policyDigest, "control_sha256": controlDigest,
		"repository_id": pubRepoID, "base_ref": "refs/heads/main", "base_sha": pubBaseSHA,
		"source_commit": pubSrcSHA, "files": files,
	}
	manifestData, _ := json.Marshal(manifest)
	manifestData = append(manifestData, '\n')
	writePubRaw(t, dir, "manifest.json", manifestData)

	return dir, pubSHA(manifestData)
}

func runPublish(t *testing.T, bin string, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()

	statePath := publisherStatePath(args)
	if statePath == "" {
		statePath = filepath.Join(t.TempDir(), "publisher-state.json")
	}

	return runDutoAI(t, bin, append([]string{"publish"}, args...), map[string]string{"DUTO_TEST_PUBLISH_STATE": statePath})
}

func publisherStatePath(args []string) string {
	for index, value := range args {
		if value == "--config" && index+1 < len(args) {
			return filepath.Join(filepath.Dir(args[index+1]), "publisher-state.json")
		}
	}

	return ""
}

func assertPublisherStateAbsent(t *testing.T, args []string) {
	t.Helper()

	path := publisherStatePath(args)
	if path == "" {
		t.Fatal("publisher state path is unavailable")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publisher adapter state exists after pre-verification rejection: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CLI surface
// ---------------------------------------------------------------------------

func TestM3Publisher_CommandExists(t *testing.T) {
	bin := dutoAIBinary(t)

	_, stderr, code := runPublish(t, bin, []string{"--help"})
	if code != 0 && strings.Contains(stderr, "unknown command") {
		t.Fatalf("missing M3 publisher behavior: 'duto-ai publish' must be a registered command\nstderr: %s", stderr)
	}
}

func TestM3Publisher_RequiredFlagsEnforced(t *testing.T) {
	bin := dutoAIBinary(t)

	const devNull = "/dev/null"

	allFlags := []string{
		"--config", devNull,
		"--control-evidence", devNull,
		"--bundle", "/tmp",
		"--expected-bundle-sha256", pubHex("a", 64),
		"--permission-profile", "reply",
		"--receipt", devNull,
	}

	required := []string{"--config", "--control-evidence", "--bundle", "--expected-bundle-sha256", "--permission-profile", "--receipt"}
	for _, missing := range required {
		t.Run(strings.TrimPrefix(missing, "--"), func(t *testing.T) {
			var args []string

			for i := 0; i < len(allFlags); i += 2 {
				if allFlags[i] != missing {
					args = append(args, allFlags[i], allFlags[i+1])
				}
			}

			_, stderr, code := runPublish(t, bin, args)
			if code == 0 {
				t.Fatalf("missing M3 publisher behavior: missing %s must be rejected; got exit 0", missing)
			}

			if strings.Contains(stderr, "unknown command") {
				t.Fatalf("missing M3 publisher behavior: publish subcommand must exist; got unknown command")
			}
		})
	}
}

func TestM3Publisher_InvalidPermissionProfileRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	args := []string{
		"--config", writePubConfig(t), "--control-evidence", "/dev/null",
		"--bundle", "/tmp", "--expected-bundle-sha256", pubHex("a", 64),
		"--permission-profile", "merge-and-release", "--receipt", filepath.Join(t.TempDir(), "receipt.json"),
	}

	_, _, code := runPublish(t, bin, args)
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: invalid permission-profile must be rejected; got exit 0")
	}

	assertPublisherStateAbsent(t, args)
}

func TestM3Publisher_ControlEvidenceStdinRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	cfgPath := writePubConfig(t)

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", "-",
		"--bundle", "/tmp", "--expected-bundle-sha256", pubHex("a", 64),
		"--permission-profile", "reply", "--receipt", "/dev/null",
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: --control-evidence - must be rejected; got exit 0")
	}
}

// ---------------------------------------------------------------------------
// Verify-before-credential: zero write counters on rejection
// ---------------------------------------------------------------------------

func TestM3Publisher_BundleSHAMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "A reply.")
	cfgPath := writePubConfig(t)
	evidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	args := []string{
		"--config", cfgPath, "--control-evidence", evidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", pubHex("0", 64),
		"--permission-profile", "reply", "--receipt", receiptPath,
	}

	stdout, stderr, code := runPublish(t, bin, args)
	if strings.Contains(stderr, "unknown command") {
		t.Fatalf("missing M3 publisher behavior: 'duto-ai publish' must be a registered command\nstderr: %s", stderr)
	}

	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: bundle SHA mismatch must be rejected; got exit 0")
	}

	if _, err := os.Stat(receiptPath); err == nil {
		t.Fatalf("missing M3 publisher behavior: no receipt must be written for rejected bundle")
	}

	assertPublisherStateAbsent(t, args)

	combined := stdout + stderr
	if !strings.Contains(combined, "rejected") && !strings.Contains(combined, "sha256") && !strings.Contains(combined, "mismatch") {
		t.Fatalf("missing M3 publisher behavior: SHA mismatch must name rejected/sha256/mismatch\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestM3Publisher_ManifestFileDigestMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "Good answer.")
	opPath := filepath.Join(bundleDir, "operations", "0001-conversation-reply.json")
	orig, _ := os.ReadFile(opPath)
	_ = os.WriteFile(opPath, append(orig, ' '), 0o644)
	manifestData, _ := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	bundleSHA := pubSHA(manifestData)

	cfgPath := writePubConfig(t)
	evidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", evidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: tampered operation file must be rejected; got exit 0")
	}

	if _, err := os.Stat(receiptPath); err == nil {
		t.Fatalf("missing M3 publisher behavior: no receipt must be written for tampered bundle")
	}
}

func TestM3Publisher_ControlDigestMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "A reply.")
	_ = os.WriteFile(filepath.Join(bundleDir, "control.json"), []byte(`{"version":1}`), 0o644)
	manifestData, _ := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	bundleSHA := pubSHA(manifestData)
	cfgPath := writePubConfig(t)
	evidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", evidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: bundled control digest mismatch must be rejected; got exit 0")
	}
}

func TestM3Publisher_ExpiredBundledControlRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	expiredEvidence := pubEvidence(t)
	expiredEvidence["admission"] = map[string]any{
		"id": pubAdmID, "correlation_key": pubCorrelKey,
		"issued_at": "2020-01-01T00:00:00Z", "expires_at": "2020-01-01T06:00:00Z",
	}
	bundleDir, bundleSHA := buildReplyBundle(t, expiredEvidence, "Expired.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, pubEvidence(t))
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: expired bundled control must be rejected; got exit 0")
	}
}

func TestM3Publisher_RepositoryIDMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	bundledEvidence := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, bundledEvidence, "A reply.")
	currentEvidence := pubEvidence(t)
	currentEvidence["repository"] = map[string]any{
		"id": "9999", "owner_id": pubOwnerID, "owner": pubOwner, "name": pubRepo, "default_branch": "main",
	}
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, currentEvidence)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	args := []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	}

	_, _, code := runPublish(t, bin, args)
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: repository ID mismatch must be rejected; got exit 0")
	}

	assertPublisherStateAbsent(t, args)
}

func TestM3Publisher_CorrelationKeyMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	bundledEvidence := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, bundledEvidence, "A reply.")
	currentEvidence := pubEvidence(t)
	adm := currentEvidence["admission"].(map[string]any)
	adm["correlation_key"] = "different-key"
	currentEvidence["admission"] = adm
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, currentEvidence)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: correlation key mismatch must be rejected; got exit 0")
	}
}

func TestM3Publisher_RunIDMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	bundledEvidence := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, bundledEvidence, "A reply.")
	currentEvidence := pubEvidence(t)
	currentEvidence["run"] = map[string]any{"id": "9999", "attempt": 1, "workflow_sha": pubWorkflow}
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, currentEvidence)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: run ID mismatch must be rejected; got exit 0")
	}
}

func TestM3Publisher_FailedBundleRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "A reply.")
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestData, _ := os.ReadFile(manifestPath)

	var manifest map[string]any

	_ = json.Unmarshal(manifestData, &manifest)
	manifest["completion"] = "failed"
	manifest["operation_set"] = "none"
	newManifest, _ := json.Marshal(manifest)
	newManifest = append(newManifest, '\n')
	_ = os.WriteFile(manifestPath, newManifest, 0o644)
	bundleSHA := pubSHA(newManifest)
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: completion=failed bundle must be rejected; got exit 0")
	}
}

func TestM3Publisher_WrongPermissionProfileIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, evidenceDoc, "A reply.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	args := []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "branch-pr", "--receipt", receiptPath,
	}

	_, _, code := runPublish(t, bin, args)
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: wrong permission-profile for operation set must be rejected; got exit 0")
	}

	assertPublisherStateAbsent(t, args)
}

func TestM3Publisher_PolicyDigestMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "A reply.")
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestData, _ := os.ReadFile(manifestPath)

	var manifest map[string]any

	_ = json.Unmarshal(manifestData, &manifest)
	manifest["policy_sha256"] = pubHex("f", 64)
	newManifest, _ := json.Marshal(manifest)
	newManifest = append(newManifest, '\n')
	_ = os.WriteFile(manifestPath, newManifest, 0o644)
	bundleSHA := pubSHA(newManifest)
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: policy_sha256 mismatch must be rejected; got exit 0")
	}
}

func TestM3Publisher_AdmissionIDMismatchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	wrongEvidence := pubEvidence(t)
	adm := wrongEvidence["admission"].(map[string]any)
	adm["id"] = "different-admission-id"
	wrongEvidence["admission"] = adm
	bundleDir, bundleSHA := buildReplyBundle(t, wrongEvidence, "A reply.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, wrongEvidence)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: admission ID mismatch must be rejected; got exit 0")
	}
}

// ---------------------------------------------------------------------------
// Receipt shape and exit codes
// ---------------------------------------------------------------------------

func TestM3Publisher_ReceiptShapeOnSuccess(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, evidenceDoc, "Please provide the package path.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	stdout, stderr, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code != 0 {
		t.Fatalf("missing M3 publisher behavior: local-valid reply bundle must succeed (exit 0); got exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	receiptData, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("missing M3 publisher behavior: receipt file must be written on success; err: %v", err)
	}

	var receipt map[string]any
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatalf("missing M3 publisher behavior: receipt must be valid JSON; err: %v\ndata: %s", err, receiptData)
	}

	for _, key := range []string{"version", "publisher_run_id", "bundle_sha256", "plan_sha256", "policy_sha256", "repository_id", "operation_set", "disposition", "operations"} {
		if _, ok := receipt[key]; !ok {
			t.Fatalf("missing M3 publisher behavior: receipt must have %q field\nreceipt: %s", key, receiptData)
		}
	}

	disposition, _ := receipt["disposition"].(string)
	if !map[string]bool{"applied": true, "unchanged": true, "rejected": true, "conflict": true}[disposition] {
		t.Fatalf("missing M3 publisher behavior: disposition must be applied/unchanged/rejected/conflict; got %q", disposition)
	}
}

func TestM3Publisher_JSONFormatWritesReceiptToStdout(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, evidenceDoc, "Answer.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	stdout, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath, "--format", "json",
	})
	if code != 0 {
		t.Fatalf("missing M3 publisher behavior: --format json run must succeed; got exit %d", code)
	}

	var receiptJSON map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &receiptJSON); err != nil {
		t.Fatalf("missing M3 publisher behavior: --format json must write receipt JSON to stdout; err: %v\nstdout: %s", err, stdout)
	}

	if _, ok := receiptJSON["disposition"]; !ok {
		t.Fatalf("missing M3 publisher behavior: stdout receipt JSON must have 'disposition'\nstdout: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Idempotency and conflict within one activation
// ---------------------------------------------------------------------------

func TestM3Publisher_IdempotentRunReturnsUnchanged(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, evidenceDoc, "The answer is 42.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	flags := []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply",
	}
	receiptPath1 := filepath.Join(t.TempDir(), "receipt1.json")

	_, _, code1 := runPublish(t, bin, append(flags, "--receipt", receiptPath1))
	if code1 != 0 {
		t.Fatalf("missing M3 publisher behavior: first run must succeed; got exit %d", code1)
	}

	receiptPath2 := filepath.Join(t.TempDir(), "receipt2.json")

	_, _, code2 := runPublish(t, bin, append(flags, "--receipt", receiptPath2))
	if code2 != 0 {
		t.Fatalf("missing M3 publisher behavior: idempotent second run must succeed; got exit %d", code2)
	}

	receiptData2, _ := os.ReadFile(receiptPath2)

	var receipt2 map[string]any

	_ = json.Unmarshal(receiptData2, &receipt2)
	if receipt2["disposition"] != "unchanged" {
		t.Fatalf("missing M3 publisher behavior: second identical run must return unchanged; got %v", receipt2["disposition"])
	}
}

func TestM3Publisher_DifferentPayloadSameCorrelationIsConflict(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir1, bundleSHA1 := buildReplyBundle(t, evidenceDoc, "First answer.")
	bundleDir2, bundleSHA2 := buildReplyBundle(t, evidenceDoc, "Completely different answer.")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)

	receiptPath1 := filepath.Join(t.TempDir(), "receipt1.json")

	_, _, code1 := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir1, "--expected-bundle-sha256", bundleSHA1,
		"--permission-profile", "reply", "--receipt", receiptPath1,
	})
	if code1 != 0 {
		t.Fatalf("missing M3 publisher behavior: first run must succeed; got exit %d", code1)
	}

	receiptPath2 := filepath.Join(t.TempDir(), "receipt2.json")

	_, _, code2 := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir2, "--expected-bundle-sha256", bundleSHA2,
		"--permission-profile", "reply", "--receipt", receiptPath2,
	})
	if code2 != 4 {
		t.Fatalf("missing M3 publisher behavior: different payload same correlation must exit 4 (conflict); got exit %d", code2)
	}

	receiptData2, _ := os.ReadFile(receiptPath2)

	var receipt2 map[string]any

	_ = json.Unmarshal(receiptData2, &receipt2)
	if receipt2["disposition"] != "conflict" {
		t.Fatalf("missing M3 publisher behavior: conflict receipt must have disposition=conflict; got %v", receipt2["disposition"])
	}
}

// ---------------------------------------------------------------------------
// Structural bundle requirements
// ---------------------------------------------------------------------------

func TestM3Publisher_ExtraFilesInBundleRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, _ := buildReplyBundle(t, evidenceDoc, "Answer.")
	_ = os.WriteFile(filepath.Join(bundleDir, "unexpected.txt"), []byte("extra"), 0o644)
	manifestData, _ := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	bundleSHA := pubSHA(manifestData)
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: extra unlisted files in bundle must be rejected; got exit 0")
	}
}

func TestM3Publisher_BundleSymlinkIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubEvidence(t)
	bundleDir, bundleSHA := buildReplyBundle(t, evidenceDoc, "Answer.")

	symlinkDir := filepath.Join(t.TempDir(), "symlink-bundle")
	if err := os.Symlink(bundleDir, symlinkDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", symlinkDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "reply", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: symlink bundle directory must be rejected; got exit 0")
	}
}

func TestM3Publisher_BranchPRRequiresAuthoredBundle(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubDispatchEvidence(t)
	validTarget := "refs/heads/duto/m3/" + pubCorrelKey
	bundleDir, bundleSHA := buildBranchPRBundleNoAuthoredBundle(t, evidenceDoc, validTarget)
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "branch-pr", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: branch-pr bundle without authored.bundle must be rejected; got exit 0")
	}
}

// ---------------------------------------------------------------------------
// Forbidden Git/GitHub operation rejections
// ---------------------------------------------------------------------------

func TestM3Publisher_DefaultBranchWriteIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubDispatchEvidence(t)
	bundleDir, bundleSHA := buildBranchPRBundleNoAuthoredBundle(t, evidenceDoc, "refs/heads/main")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	args := []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "branch-pr", "--receipt", receiptPath,
	}

	_, _, code := runPublish(t, bin, args)
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: default branch write must be rejected; got exit 0")
	}

	assertPublisherStateAbsent(t, args)
}

func TestM3Publisher_NonNamespacedBranchIsRejected(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubDispatchEvidence(t)
	bundleDir, bundleSHA := buildBranchPRBundleNoAuthoredBundle(t, evidenceDoc, "refs/heads/my-feature-branch")
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "branch-pr", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: non-namespaced branch must be rejected; got exit 0")
	}
}

func TestM3Publisher_BranchBeforePROrdering(t *testing.T) {
	bin := dutoAIBinary(t)
	evidenceDoc := pubDispatchEvidence(t)
	bundleDir, bundleSHA := buildPROnlyBundle(t, evidenceDoc)
	cfgPath := writePubConfig(t)
	currentEvidencePath := writePubEvidenceFile(t, evidenceDoc)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	_, _, code := runPublish(t, bin, []string{
		"--config", cfgPath, "--control-evidence", currentEvidencePath,
		"--bundle", bundleDir, "--expected-bundle-sha256", bundleSHA,
		"--permission-profile", "branch-pr", "--receipt", receiptPath,
	})
	if code == 0 {
		t.Fatalf("missing M3 publisher behavior: draft PR without branch dependency must be rejected; got exit 0")
	}
}
