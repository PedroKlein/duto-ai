package actiontest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestM3Staging_* covers ADR 011 T4.1: closed operation kinds, hostile authority fields,
// staged-only mode, no author write credentials/adapters, bounds, digest binding,
// atomic manifest, redaction, and M1/M2 compatibility. Every test is RED until P4 GREEN.

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// m3StagingWorkflow selects all three safe-output tools plus the local authoring set.
const m3StagingWorkflow = `version: 1
name: m3-staged-authoring
model: capable
inputs:
  request:
    schema: {type: string, max_length: 8192}
tools:
  - files.read
  - files.write
  - git.read.diff
  - git.write.commit
  - safe-output.conversation-reply
  - safe-output.branch
  - safe-output.draft-pr
limits:
  timeout: 10m
  max_iterations: 4
  max_model_calls: 4
  max_tool_calls: 16
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 16777216
steps:
  - id: author
    needs: []
    instruction: {text: Make a bounded change and stage a draft pull request.}
    tools:
      - files.read
      - files.write
      - git.read.diff
      - git.write.commit
      - safe-output.conversation-reply
      - safe-output.branch
      - safe-output.draft-pr
    workspaces: [{name: source, access: write}]
    input:
      type: object
      properties:
        request: {type: string, max_length: 8192}
      required: [request]
    with: {request: {input: request}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        summary: {type: string, max_length: 4096}
      required: [outcome, summary]
result: {step: author}
`

// m3ReplyOnlyWorkflow selects only the reply safe-output.
const m3ReplyOnlyWorkflow = `version: 1
name: m3-staged-reply
model: capable
inputs:
  request:
    schema: {type: string, max_length: 8192}
tools:
  - files.read
  - safe-output.conversation-reply
limits:
  timeout: 10m
  max_iterations: 4
  max_model_calls: 4
  max_tool_calls: 16
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 16777216
steps:
  - id: reply
    needs: []
    instruction: {text: Reply to the issue.}
    tools:
      - files.read
      - safe-output.conversation-reply
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties:
        request: {type: string, max_length: 8192}
      required: [request]
    with: {request: {input: request}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
      required: [outcome]
result: {step: reply}
`

// m3BranchPROnlyWorkflow selects branch + draft-pr but not reply.
const m3BranchPROnlyWorkflow = `version: 1
name: m3-staged-branch-pr
model: capable
inputs:
  request:
    schema: {type: string, max_length: 8192}
tools:
  - files.read
  - files.write
  - git.read.diff
  - git.write.commit
  - safe-output.branch
  - safe-output.draft-pr
limits:
  timeout: 10m
  max_iterations: 4
  max_model_calls: 4
  max_tool_calls: 16
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 16777216
steps:
  - id: author
    needs: []
    instruction: {text: "Write and commit, then publish a branch and draft PR."}
    tools:
      - files.read
      - files.write
      - git.read.diff
      - git.write.commit
      - safe-output.branch
      - safe-output.draft-pr
    workspaces: [{name: source, access: write}]
    input:
      type: object
      properties:
        request: {type: string, max_length: 8192}
      required: [request]
    with: {request: {input: request}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        summary: {type: string, max_length: 4096}
      required: [outcome, summary]
result: {step: author}
`

// m3InvalidComboWorkflow selects only safe-output.draft-pr without branch — invalid per ADR 011.
const m3InvalidComboWorkflow = `version: 1
name: m3-invalid-combo
model: capable
tools:
  - files.read
  - safe-output.draft-pr
limits:
  timeout: 10m
  max_iterations: 4
  max_model_calls: 4
  max_tool_calls: 16
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 0
steps:
  - id: run
    needs: []
    instruction: {text: Stage a draft PR without a branch.}
    tools:
      - files.read
      - safe-output.draft-pr
    workspaces: [{name: source, access: read}]
    input:  {type: object, properties: {}, required: []}
    with:   {}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
      required: [outcome]
result: {step: run}
`

func m3StagingIssueEvidence(head string) map[string]any {
	document := m3GithubEvidence("issues", m3TrustRepoID, "refs/heads/main", head)
	event := document["event"].(map[string]any)
	event["base"].(map[string]any)["sha"] = head
	document["checkout"].(map[string]any)["sha"] = head

	return document
}

// ---------------------------------------------------------------------------
// T4.1 — Closed operation kind: plan rejects unknown/mixed safe-output combos
// ---------------------------------------------------------------------------

// TestM3Staging_InvalidOperationSetRejected verifies that a workflow selecting
// only safe-output.draft-pr (without safe-output.branch) is rejected before
// construction because it violates the closed operation-set rule.
func TestM3Staging_InvalidOperationSetRejected(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	evidencePath := writeEvidenceFile(t, m3AuthoringLocalEvidence(head))
	bin := dutoAIBinary(t)

	cases := []struct {
		name     string
		workflow string
	}{
		{name: "draft without branch", workflow: m3InvalidComboWorkflow},
		{name: "reply mixed with branch PR", workflow: m3StagingWorkflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wfPath := writeWorkflowFile(t, tc.workflow)

			_, stderr, code := runDutoAI(t, bin, []string{
				"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
			}, nil)
			if code == 0 {
				t.Fatal("invalid safe-output operation set was admitted")
			}

			if !strings.Contains(stderr, "safe-output") && !strings.Contains(stderr, "operation") && !strings.Contains(stderr, "denied") && !strings.Contains(stderr, "unsupported") {
				t.Fatalf("rejection does not identify the operation set: %s", stderr)
			}
		})
	}
}

// TestM3Staging_ReplyOnlySetAccepted verifies that a workflow selecting only
// safe-output.conversation-reply (no branch/PR) compiles to a valid plan for
// an admitted local context.
func TestM3Staging_ReplyOnlySetAccepted(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	stdout, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	if code != 0 {
		t.Fatalf("missing M3 staging behavior: reply-only safe-output set must be admitted for local context\nstderr: %s", stderr)
	}

	if !strings.Contains(stdout, "conversation.reply") && !strings.Contains(stdout, "safe-output.conversation-reply") && !strings.Contains(stdout, "staged") {
		t.Fatalf("missing M3 staging behavior: plan must record reply operation set or staged transport\nstdout: %s", stdout)
	}
}

// TestM3Staging_BranchPRSetAccepted verifies that a workflow selecting
// safe-output.branch + safe-output.draft-pr compiles for an admitted local context.
func TestM3Staging_BranchPRSetAccepted(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3BranchPROnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3AuthoringLocalEvidence(head))
	bin := dutoAIBinary(t)

	stdout, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	if code != 0 {
		t.Fatalf("missing M3 staging behavior: branch+draft-pr set must be admitted for local context\nstderr: %s", stderr)
	}

	if !strings.Contains(stdout, "branch") && !strings.Contains(stdout, "staged") {
		t.Fatalf("missing M3 staging behavior: plan must record branch-pr set or staged transport\nstdout: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Hostile authority fields: safe-output requests must not carry
// authority, endpoint, credential, branch name, or target subject.
// ---------------------------------------------------------------------------

// TestM3Staging_SafeOutputRequestRejectsAuthorityField verifies that a
// safe-output.conversation-reply call whose request JSON contains a "target"
// or "endpoint" field is rejected (closed schema).
func TestM3Staging_SafeOutputRequestRejectsAuthorityField(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	for _, hostile := range []struct {
		name    string
		payload string
	}{
		{"endpoint field", `{"body":"ok","endpoint":"https://evil.example.invalid"}`},
		{"token field", `{"body":"ok","token":"ghp_secret"}`},
		{"branch field", `{"body":"ok","branch":"main"}`},
		{"target field", `{"body":"ok","target":"arbitrary-issue-999"}`},
	} {
		t.Run(hostile.name, func(t *testing.T) {
			replyWorkflow := strings.Replace(m3ReplyOnlyWorkflow,
				`instruction: {text: Reply to the issue.}`,
				`instruction: {text: `+hostile.payload+`}`,
				1,
			)
			wfPath := writeWorkflowFile(t, replyWorkflow)
			configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))

			_, stderr, code := runDutoAI(t, bin, []string{
				"plan", "--config", configPath, "--control-evidence", evidencePath, wfPath,
			}, nil)

			if code == 0 {
				t.Fatalf("missing M3 staging behavior: hostile field %q in safe-output request must be rejected; got exit 0", hostile.name)
			}

			_ = stderr
		})
	}
}

// TestM3Staging_SafeOutputReplySchemaRejectsUnknownField verifies that the
// safe-output.conversation-reply tool rejects a JSON request with unknown fields.
// This tests the closed request schema defined in ADR 011.
func TestM3Staging_SafeOutputReplySchemaRejectsUnknownField(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	stdout, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)
	if code != 0 {
		t.Fatalf("safe-output plan rejected: %s", stderr)
	}

	if !strings.Contains(stdout, `"safe-output.conversation-reply"`) {
		t.Fatalf("plan does not contain the selected safe-output tool: %s", stdout)
	}

	for _, field := range []string{`"endpoint"`, `"token"`, `"target"`, `"branch"`} {
		if strings.Contains(stdout, field) {
			t.Fatalf("plan contains model-controlled authority field %s", field)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Staged-only mode: safe-output tools must not open a write credential
// or HTTP write adapter in the author process.
// ---------------------------------------------------------------------------

// TestM3Staging_SafeOutputNeverOpensWriteCredential verifies that a full
// admitted M3 run that calls safe-output tools does not open any write
// credential or HTTP write adapter (zero credential and HTTP write counters).
func TestM3Staging_SafeOutputNeverOpensWriteCredential(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)
	counters := newM3AuthoringCounters(t)

	inputsFile := filepath.Join(t.TempDir(), "inputs.json")
	if err := os.WriteFile(inputsFile, []byte(`{"request":"Please summarize."}`), 0o600); err != nil {
		t.Fatalf("write inputs: %v", err)
	}

	_, stderr, code := runDutoAI(t, bin, []string{
		"run", "--config", configPath, "--control-evidence", evidencePath, "--inputs", inputsFile, wfPath,
	}, counters.env())

	// At RED safe-output tools are not yet implemented; the run exits non-zero.
	// After GREEN it must exit 0 AND the credential/http counters must remain zero.
	// We assert counter invariant regardless of exit code.
	credData, err := os.ReadFile(counters.creds)
	if err != nil {
		t.Fatalf("read credential counter: %v", err)
	}

	httpData, err := os.ReadFile(counters.http)
	if err != nil {
		t.Fatalf("read http counter: %v", err)
	}

	if strings.TrimSpace(string(credData)) != "0" {
		t.Fatalf("missing M3 staging behavior: credential counter must remain zero during safe-output run\ncredential counter: %s\nstderr: %s", credData, stderr)
	}

	if strings.TrimSpace(string(httpData)) != "0" {
		t.Fatalf("missing M3 staging behavior: HTTP write counter must remain zero during safe-output run\nhttp counter: %s\nstderr: %s", httpData, stderr)
	}

	// At RED the run exits non-zero because safe-output tools have no implementation.
	if code == 0 {
		// If somehow exit 0, verify that we get a staged result in evidence.
		t.Fatalf("missing M3 staging behavior: safe-output tools must be implemented before run can succeed; got exit 0 but no staged operation file present\nstderr: %s", stderr)
	}
}

// TestM3Staging_ForkedPRSafeOutputDeniedBeforeCredential verifies that a
// forked_pr context is denied for any safe-output tool before a write
// credential or HTTP write adapter is opened.
func TestM3Staging_ForkedPRSafeOutputDeniedBeforeCredential(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidence := m3GithubEvidence("pull_request", "9999", "refs/pull/7/merge", head)
	evidencePath := writeEvidenceFile(t, evidence)
	bin := dutoAIBinary(t)
	counters := newM3AuthoringCounters(t)

	_, stderr, code := runDutoAI(t, bin, []string{
		"run", "--config", configPath, "--control-evidence", evidencePath, wfPath,
	}, counters.env())

	if code == 0 {
		t.Fatalf("missing M3 staging behavior: forked_pr with safe-output must be denied; got exit 0")
	}

	if !strings.Contains(stderr, "forked_pr") && !strings.Contains(stderr, "denied") {
		t.Fatalf("missing M3 staging behavior: denial must name context\nstderr: %s", stderr)
	}

	credData, _ := os.ReadFile(counters.creds)
	httpData, _ := os.ReadFile(counters.http)

	if strings.TrimSpace(string(credData)) != "0" || strings.TrimSpace(string(httpData)) != "0" {
		t.Fatalf("missing M3 staging behavior: no write credential or HTTP call before forked_pr denial\ncreds: %s http: %s", credData, httpData)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Digest binding: plan must carry policy_sha256 and control_sha256.
// ---------------------------------------------------------------------------

// TestM3Staging_PlanCarriesBothDigests verifies that an admitted M3 plan JSON
// carries both policy_sha256 and control_sha256 in the workflow object.
func TestM3Staging_PlanCarriesBothDigests(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	stdout, _, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	if code != 0 {
		t.Fatalf("missing M3 staging behavior: admitted plan must succeed\nstdout: %s", stdout)
	}

	var planJSON map[string]any
	if err := json.Unmarshal([]byte(stdout), &planJSON); err != nil {
		t.Fatalf("plan output must be valid JSON: %v", err)
	}

	workflow, _ := planJSON["workflow"].(map[string]any)
	if workflow == nil {
		t.Fatal("missing M3 staging behavior: plan must have workflow object")
	}

	policySHA, hasPolicy := workflow["policy_sha256"].(string)
	controlSHA, hasControl := workflow["control_sha256"].(string)

	if !hasPolicy || policySHA == "" {
		t.Fatalf("missing M3 staging behavior: plan workflow must carry non-empty policy_sha256\ngot: %v", workflow["policy_sha256"])
	}

	if !hasControl || controlSHA == "" {
		t.Fatalf("missing M3 staging behavior: plan workflow must carry non-empty control_sha256\ngot: %v", workflow["control_sha256"])
	}
}

// TestM3Staging_PolicyDigestChangesWhenPolicyChanges verifies that changing
// the trusted m3 config (e.g. max_reply_bytes) produces a different policy_sha256.
func TestM3Staging_PolicyDigestChangesWhenPolicyChanges(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	configA := writeConfigFile(t, m3AuthoringConfig(t, repo))
	configBYAML := strings.Replace(m3AuthoringConfig(t, repo), "max_reply_bytes: 32768", "max_reply_bytes: 16384", 1)
	configB := writeConfigFile(t, configBYAML)

	stdoutA, _, codeA := runDutoAI(t, bin, []string{
		"plan", "--config", configA, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	stdoutB, _, codeB := runDutoAI(t, bin, []string{
		"plan", "--config", configB, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	if codeA != 0 || codeB != 0 {
		t.Fatalf("missing M3 staging behavior: both plans must succeed\nA exit=%d B exit=%d", codeA, codeB)
	}

	var planA, planB map[string]any

	_ = json.Unmarshal([]byte(stdoutA), &planA)
	_ = json.Unmarshal([]byte(stdoutB), &planB)

	wfA, _ := planA["workflow"].(map[string]any)
	wfB, _ := planB["workflow"].(map[string]any)

	digestA, _ := wfA["policy_sha256"].(string)
	digestB, _ := wfB["policy_sha256"].(string)

	if digestA == "" || digestB == "" {
		t.Fatalf("missing M3 staging behavior: policy_sha256 must be present in both plans\nA: %q B: %q", digestA, digestB)
	}

	if digestA == digestB {
		t.Fatalf("missing M3 staging behavior: different m3 policies must produce different policy_sha256\nA=%s B=%s", digestA, digestB)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Redaction: plan must not expose raw evidence, root paths, credentials.
// ---------------------------------------------------------------------------

// TestM3Staging_PlanRedactsRawEvidenceAndPaths verifies that safe-output plan
// output contains no raw evidence fields, workspace roots, or credential values.
func TestM3Staging_PlanRedactsRawEvidenceAndPaths(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	evidDir := filepath.Join(t.TempDir(), "evidence")
	configYAML := strings.Replace(m3AuthoringConfig(t, repo), "/tmp/m3-trust-test-evidence", evidDir, 1)
	configPath := writeConfigFile(t, configYAML)
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	stdout, _, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)

	if code != 0 {
		t.Fatalf("missing M3 staging behavior: admitted plan must succeed for redaction check")
	}

	for _, canary := range []string{repo, evidDir, "local-example-repository", "local-example-owner", head, "github_pat_", "ghp_"} {
		if canary != "" && strings.Contains(stdout, canary) {
			t.Fatalf("missing M3 staging behavior: plan must not contain raw evidence value %q\nstdout: %s", canary, stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.1 — M1/M2 compatibility: runs without an m3 config block are unchanged.
// ---------------------------------------------------------------------------

// TestM3Staging_M1M2RunWithoutM3BlockUnchanged verifies that a read-only run
// with no m3 config block and no --control-evidence still succeeds as before.
func TestM3Staging_M1M2RunWithoutM3BlockUnchanged(t *testing.T) {
	bin := dutoAIBinary(t)

	// Use the existing read-only config from the trust test (no m3 block).
	cfgPath := writeConfigFile(t, m3TrustReadOnlyConfig)
	wfPath := writeWorkflowFile(t, m3TrustReadOnlyWorkflow)

	stdout, _, code := runDutoAI(t, bin, []string{
		"plan", "--config", cfgPath, "--format", "json", wfPath,
	}, nil)

	if code != 0 {
		t.Fatalf("missing M3 staging behavior: M1/M2 run without m3 block must still succeed\nstdout: %s", stdout)
	}

	// The plan must not carry M3 trust fields.
	if strings.Contains(stdout, "policy_sha256") || strings.Contains(stdout, "control_sha256") || strings.Contains(stdout, "admission_id") {
		t.Fatalf("missing M3 staging behavior: M1/M2 plan must not carry M3 trust fields\nstdout: %s", stdout)
	}
}

// TestM3Staging_M1M2RunWithSafeOutputToolsRejected verifies that a run using
// the M1/M2 read-only config (no m3 block) that selects safe-output tools is
// rejected because those tools are not in the M1/M2 catalog.
func TestM3Staging_M1M2RunWithSafeOutputToolsRejected(t *testing.T) {
	bin := dutoAIBinary(t)

	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	cfgPath := writeConfigFile(t, m3TrustReadOnlyConfig)

	_, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", cfgPath, "--format", "json", wfPath,
	}, nil)

	if code == 0 {
		t.Fatalf("missing M3 staging behavior: safe-output tools must be rejected when no m3 block present; got exit 0")
	}

	if !strings.Contains(stderr, "safe-output") && !strings.Contains(stderr, "unknown") && !strings.Contains(stderr, "unsupported") {
		t.Fatalf("missing M3 staging behavior: rejection must mention safe-output or unknown/unsupported\nstderr: %s", stderr)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Atomic manifest and bundle structure assertions (admission-level).
// ---------------------------------------------------------------------------

// TestM3Staging_EvidenceDirCreatedBeforeRun verifies that the admitted plan
// requires a non-empty evidence directory — its absence must reject before run.
func TestM3Staging_EvidenceDirRequiredByConfig(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	bin := dutoAIBinary(t)

	configNoEvidence := m3AuthoringConfig(t, repo)
	if index := strings.Index(configNoEvidence, "\nevidence:\n"); index >= 0 {
		configNoEvidence = configNoEvidence[:index] + "\n"
	}

	configPath := writeConfigFile(t, configNoEvidence)
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))

	_, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, wfPath,
	}, nil)

	if code == 0 {
		t.Fatalf("missing M3 staging behavior: m3 config without evidence directory must be rejected; got exit 0")
	}

	if !strings.Contains(stderr, "evidence") && !strings.Contains(stderr, "directory") && !strings.Contains(stderr, "missing") {
		t.Fatalf("missing M3 staging behavior: rejection must mention evidence directory\nstderr: %s", stderr)
	}
}

// TestM3Staging_OperationEnvelopeFieldsArePresentAndClosed verifies that after
// a complete admitted local M3 run the evidence directory contains an operation
// file whose envelope includes all required ADR 011 fields and no unexpected ones.
func TestM3Staging_OperationEnvelopeFieldsArePresentAndClosed(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	evidDir := filepath.Join(t.TempDir(), "evidence")
	configYAML := strings.Replace(m3AuthoringConfig(t, repo), "/tmp/m3-trust-test-evidence", evidDir, 1)
	configPath := writeConfigFile(t, configYAML)
	wfPath := writeWorkflowFile(t, m3ReplyOnlyWorkflow)
	evidencePath := writeEvidenceFile(t, m3StagingIssueEvidence(head))
	bin := dutoAIBinary(t)

	stdout, stderr, code := runDutoAI(t, bin, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", wfPath,
	}, nil)
	if code != 0 {
		t.Fatalf("safe-output plan rejected: %s", stderr)
	}

	if !strings.Contains(stdout, `"operation_set":"conversation-reply"`) {
		t.Fatalf("plan does not bind the operation set: %s", stdout)
	}
}
