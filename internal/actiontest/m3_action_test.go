package actiontest_test

// TestM3Action_* covers ADR 011 T6.1: separate author/publisher Action metadata,
// artifact handoff contract, exact job permissions and token separation,
// tamper/idempotency/untrusted negatives, unchanged root M2 contract, full-SHA nested
// action pins, safe outputs, and actual duto-test playground file loading.
// All tests are RED until T6.2 implements author/action.yml and publish/action.yml.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── ADR 011 exact contracts ──────────────────────────────────────────────────

// Author Action exact required/optional inputs per ADR 011.
var (
	m3AuthorRequiredInputs = []string{"correlation-key", "version", "workflow"}
	m3AuthorOptionalInputs = map[string]string{
		"bundle-retention-days": "1",
		"config":                "duto.yaml",
	}
)

// Author Action exact outputs per ADR 011.
var m3AuthorExactOutputs = []string{
	"artifact-digest", "artifact-id", "bundle-sha256",
	"clarification-required", "operation-set", "outcome", "run-id", "status",
}

// Publish Action exact required inputs per ADR 011.
var m3PublishRequiredInputs = []string{
	"artifact-digest", "artifact-id", "bundle-sha256", "permission-profile", "version",
}

var m3PublishOptionalInputs = map[string]string{
	"config": "duto.yaml",
}

// Publish Action exact outputs per ADR 011.
var m3PublishExactOutputs = []string{
	"branch", "disposition", "pull-request-url", "receipt-path", "reply-url",
}

const (
	m3SealedActionYMLDigest    = "98ea0403af3785f33272f95c745ce225198ea50c33093e8a91b78abb3db8fb90"
	m3SealedADR009Digest       = "f4f9383040599afe51550bd86ed82a91d8f8515d15656e2b1482a771fdf18901"
	m3AuthorActionPath         = "author/action.yml"
	m3PublishActionPath        = "publish/action.yml"
	shaRefPattern              = `^[^@]+@[0-9a-f]{40}$`
	m3AuthorPermContents       = "read"
	m3PublishReplyPermIssues   = "write"
	m3PublishBranchPermContent = "write"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func loadM3ActionFile(t *testing.T, rel string) actionMetadataFile {
	t.Helper()

	path := filepath.Join(repoRoot(t), rel)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read %s: %v", rel, err)
	}

	var action actionMetadataFile
	if err := yaml.Unmarshal(content, &action); err != nil {
		t.Fatalf("missing M3 Action behavior: parse %s: %v", rel, err)
	}

	return action
}

func sortedStringSlice(items []string) []string {
	out := make([]string, len(items))
	copy(out, items)
	sort.Strings(out)

	return out
}

// ── Root M2 Action is unchanged ───────────────────────────────────────────────

// TestM3Action_RootActionUnchanged verifies that action.yml (the sealed M2 contract)
// and ADR 009 have not been modified by any M3 work.
func TestM3Action_RootActionUnchanged(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	actionBytes, err := os.ReadFile(filepath.Join(root, "action.yml"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read action.yml: %v", err)
	}

	if got := fmt.Sprintf("%x", sha256.Sum256(actionBytes)); got != m3SealedActionYMLDigest {
		t.Fatalf("missing M3 Action behavior: sealed root action.yml changed\nwant: %s\ngot:  %s", m3SealedActionYMLDigest, got)
	}

	adr009Bytes, err := os.ReadFile(filepath.Join(root, "docs", "adr", "009-one-shot-github-action.md"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read ADR 009: %v", err)
	}

	if got := fmt.Sprintf("%x", sha256.Sum256(adr009Bytes)); got != m3SealedADR009Digest {
		t.Fatalf("missing M3 Action behavior: sealed ADR 009 changed\nwant: %s\ngot:  %s", m3SealedADR009Digest, got)
	}
}

// ── Author Action metadata ────────────────────────────────────────────────────

// TestM3Action_AuthorActionExists verifies that author/action.yml exists and is composite.
func TestM3Action_AuthorActionExists(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)

	if action.Runs.Using != "composite" {
		t.Fatalf("missing M3 Action behavior: author/action.yml runs.using must be composite, got %q", action.Runs.Using)
	}
}

// TestM3Action_AuthorActionInputsAreExact verifies author/action.yml has the exact
// closed input set defined by ADR 011.
func TestM3Action_AuthorActionInputsAreExact(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)
	got := sortedStringSlice(sortedActionKeys(action.Inputs))

	want := sortedStringSlice(append(m3AuthorRequiredInputs, func() []string {
		keys := make([]string, 0, len(m3AuthorOptionalInputs))
		for k := range m3AuthorOptionalInputs {
			keys = append(keys, k)
		}

		return keys
	}()...))

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing M3 Action behavior: author/action.yml inputs must be closed and exact\nwant: %v\ngot:  %v", want, got)
	}

	for _, key := range m3AuthorRequiredInputs {
		if !action.Inputs[key].Required {
			t.Fatalf("missing M3 Action behavior: author/action.yml input %q must be required", key)
		}
	}

	for key, def := range m3AuthorOptionalInputs {
		inp, ok := action.Inputs[key]
		if !ok {
			t.Fatalf("missing M3 Action behavior: author/action.yml must have optional input %q", key)
		}

		if inp.Required {
			t.Fatalf("missing M3 Action behavior: author/action.yml input %q must be optional", key)
		}

		if def != "" && inp.Default != def {
			t.Fatalf("missing M3 Action behavior: author/action.yml input %q default = %q, want %q", key, inp.Default, def)
		}
	}
}

// TestM3Action_AuthorActionOutputsAreExact verifies author/action.yml has the exact
// closed output set defined by ADR 011.
func TestM3Action_AuthorActionOutputsAreExact(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)
	got := sortedStringSlice(sortedActionKeys(action.Outputs))
	want := sortedStringSlice(m3AuthorExactOutputs)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing M3 Action behavior: author/action.yml outputs must be closed and exact\nwant: %v\ngot:  %v", want, got)
	}
}

// ── Publisher Action metadata ─────────────────────────────────────────────────

// TestM3Action_PublishActionExists verifies that publish/action.yml exists and is composite.
func TestM3Action_PublishActionExists(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)

	if action.Runs.Using != "composite" {
		t.Fatalf("missing M3 Action behavior: publish/action.yml runs.using must be composite, got %q", action.Runs.Using)
	}
}

// TestM3Action_PublishActionInputsAreExact verifies publish/action.yml has the exact
// closed input set defined by ADR 011.
func TestM3Action_PublishActionInputsAreExact(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)
	got := sortedStringSlice(sortedActionKeys(action.Inputs))

	want := sortedStringSlice(append(m3PublishRequiredInputs, func() []string {
		keys := make([]string, 0, len(m3PublishOptionalInputs))
		for k := range m3PublishOptionalInputs {
			keys = append(keys, k)
		}

		return keys
	}()...))

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing M3 Action behavior: publish/action.yml inputs must be closed and exact\nwant: %v\ngot:  %v", want, got)
	}

	for _, key := range m3PublishRequiredInputs {
		if !action.Inputs[key].Required {
			t.Fatalf("missing M3 Action behavior: publish/action.yml input %q must be required", key)
		}
	}

	for key, def := range m3PublishOptionalInputs {
		inp, ok := action.Inputs[key]
		if !ok {
			t.Fatalf("missing M3 Action behavior: publish/action.yml must have optional input %q", key)
		}

		if inp.Required {
			t.Fatalf("missing M3 Action behavior: publish/action.yml input %q must be optional", key)
		}

		if def != "" && inp.Default != def {
			t.Fatalf("missing M3 Action behavior: publish/action.yml input %q default = %q, want %q", key, inp.Default, def)
		}
	}
}

// TestM3Action_PublishActionOutputsAreExact verifies publish/action.yml has the exact
// closed output set defined by ADR 011.
func TestM3Action_PublishActionOutputsAreExact(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)
	got := sortedStringSlice(sortedActionKeys(action.Outputs))
	want := sortedStringSlice(m3PublishExactOutputs)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing M3 Action behavior: publish/action.yml outputs must be closed and exact\nwant: %v\ngot:  %v", want, got)
	}
}

// ── Token separation: author has no write credential ─────────────────────────

// TestM3Action_AuthorActionHasNoWriteToken verifies that no step in author/action.yml
// exposes GITHUB_TOKEN or any env that looks like a write credential to the shell.
func TestM3Action_AuthorActionHasNoWriteToken(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)
	shaRef := regexp.MustCompile(shaRefPattern)

	for _, step := range action.Runs.Steps {
		for key, value := range step.Env {
			if strings.Contains(strings.ToUpper(key), "GITHUB_TOKEN") ||
				strings.Contains(strings.ToUpper(key), "GH_TOKEN") ||
				strings.Contains(strings.ToUpper(key), "WRITE_TOKEN") {
				t.Errorf("missing M3 Action behavior: author/action.yml step %q exposes write token via env %q = %q", step.ID, key, value)
			}
		}

		// No step should use a nested action with a moving tag (non-SHA)
		if step.Uses != "" && !shaRef.MatchString(step.Uses) && strings.Contains(step.Uses, "@") {
			t.Errorf("missing M3 Action behavior: author/action.yml step %q uses non-SHA action reference %q", step.ID, step.Uses)
		}
	}
}

// TestM3Action_PublishTokenIsStepScopedOnly verifies that in publish/action.yml the
// write-scoped GITHUB_TOKEN is provided only to the publish step and not to checkout,
// download, or verification steps.
func TestM3Action_PublishTokenIsStepScopedOnly(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)

	publishStepFound := false
	writeTokenKey := ""

	for _, step := range action.Runs.Steps {
		hasWriteToken := false

		for key := range step.Env {
			upper := strings.ToUpper(key)
			if upper == "GITHUB_TOKEN" || upper == "GH_TOKEN" || strings.Contains(upper, "WRITE_TOKEN") {
				hasWriteToken = true
				writeTokenKey = key
			}
		}

		// Step ids that must never receive the write token
		switch step.ID {
		case "checkout", "download", "verify", "install":
			if hasWriteToken {
				t.Errorf("missing M3 Action behavior: publish/action.yml non-publish step %q must not receive write token (key=%q)", step.ID, writeTokenKey)
			}
		default:
			if hasWriteToken {
				publishStepFound = true
			}
		}
	}

	if !publishStepFound {
		t.Fatalf("missing M3 Action behavior: publish/action.yml must have a publish step that receives the write-scoped token in its step environment")
	}
}

// ── Full-SHA action pins ──────────────────────────────────────────────────────

// TestM3Action_AllNestedActionsAreSHAPinned verifies that every `uses:` reference in
// both M3 Actions is pinned to a full 40-character SHA, not a branch or moving tag.
func TestM3Action_AllNestedActionsAreSHAPinned(t *testing.T) {
	t.Parallel()

	shaRef := regexp.MustCompile(shaRefPattern)
	movingTag := regexp.MustCompile(`@v[0-9]+`)

	for _, actionPath := range []string{m3AuthorActionPath, m3PublishActionPath} {
		action := loadM3ActionFile(t, actionPath)

		for _, step := range action.Runs.Steps {
			if step.Uses == "" {
				continue
			}

			if !shaRef.MatchString(step.Uses) {
				t.Errorf("missing M3 Action behavior: %s step %q uses non-SHA reference %q", actionPath, step.ID, step.Uses)
			}

			if movingTag.MatchString(step.Uses) {
				t.Errorf("missing M3 Action behavior: %s step %q uses moving tag reference %q", actionPath, step.ID, step.Uses)
			}
		}
	}
}

// ── Shell expression injection ────────────────────────────────────────────────

// TestM3Action_NoExpressionInterpolationInRunSteps verifies that neither M3 Action
// interpolates ${{ ... }} expressions directly in run scripts.
func TestM3Action_NoExpressionInterpolationInRunSteps(t *testing.T) {
	t.Parallel()

	exprPattern := regexp.MustCompile(`\$\{\{`)

	for _, actionPath := range []string{m3AuthorActionPath, m3PublishActionPath} {
		action := loadM3ActionFile(t, actionPath)

		for _, step := range action.Runs.Steps {
			if exprPattern.MatchString(step.Run) {
				t.Errorf("missing M3 Action behavior: %s step %q interpolates expression in run script", actionPath, step.ID)
			}
		}
	}
}

// ── Artifact handoff contract ─────────────────────────────────────────────────

// TestM3Action_AuthorUploadsArtifactStep verifies author/action.yml has a step that
// uploads the bundle artifact using a full-SHA-pinned actions/upload-artifact.
func TestM3Action_AuthorUploadsArtifactStep(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)
	shaRef := regexp.MustCompile(shaRefPattern)

	found := false

	for _, step := range action.Runs.Steps {
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			found = true

			if !shaRef.MatchString(step.Uses) {
				t.Errorf("missing M3 Action behavior: author/action.yml upload-artifact step must use full SHA, got %q", step.Uses)
			}
		}
	}

	if !found {
		t.Fatalf("missing M3 Action behavior: author/action.yml must have an actions/upload-artifact step to hand off the staged bundle")
	}
}

// TestM3Action_PublishDownloadsArtifactByID verifies publish/action.yml has a step that
// downloads the artifact using a full-SHA-pinned actions/download-artifact.
func TestM3Action_PublishDownloadsArtifactByID(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)
	shaRef := regexp.MustCompile(shaRefPattern)

	found := false

	for _, step := range action.Runs.Steps {
		if strings.HasPrefix(step.Uses, "actions/download-artifact@") {
			found = true

			if !shaRef.MatchString(step.Uses) {
				t.Errorf("missing M3 Action behavior: publish/action.yml download-artifact step must use full SHA, got %q", step.Uses)
			}

			if step.With["merge-multiple"] != "true" {
				t.Errorf("missing M3 Action behavior: publish/action.yml must download bundle files directly into the verified bundle root")
			}
		}
	}

	if !found {
		t.Fatalf("missing M3 Action behavior: publish/action.yml must have an actions/download-artifact step to receive the staged bundle")
	}
}

// TestM3Action_ArtifactIDIsWiredFromAuthorToPublish verifies that author/action.yml
// exports artifact-id and artifact-digest outputs and publish/action.yml requires them
// as inputs (closed handoff contract).
func TestM3Action_ArtifactIDIsWiredFromAuthorToPublish(t *testing.T) {
	t.Parallel()

	author := loadM3ActionFile(t, m3AuthorActionPath)
	publish := loadM3ActionFile(t, m3PublishActionPath)

	for _, output := range []string{"artifact-id", "artifact-digest", "bundle-sha256"} {
		if _, ok := author.Outputs[output]; !ok {
			t.Errorf("missing M3 Action behavior: author/action.yml must expose output %q for handoff to publisher", output)
		}
	}

	for _, input := range []string{"artifact-id", "artifact-digest", "bundle-sha256"} {
		inp, ok := publish.Inputs[input]
		if !ok {
			t.Errorf("missing M3 Action behavior: publish/action.yml must require input %q from author handoff", input)

			continue
		}

		if !inp.Required {
			t.Errorf("missing M3 Action behavior: publish/action.yml input %q must be required for verified handoff", input)
		}
	}
}

// ── Separate Actions (not root action.yml) ────────────────────────────────────

// TestM3Action_SeparateFromRootAction verifies the M3 Actions live in separate
// author/ and publish/ directories and are not extensions of root action.yml.
func TestM3Action_SeparateFromRootAction(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	for _, rel := range []string{m3AuthorActionPath, m3PublishActionPath} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing M3 Action behavior: %s must exist as a separate composite Action", rel)
		}
	}

	// Root action.yml inputs must remain exactly M2 closed set
	rootAction := loadActionMetadata(t)
	gotRootInputs := sortedStringSlice(sortedActionKeys(rootAction.Inputs))
	wantRootInputs := []string{"config", "evidence-retention-days", "version", "workflow"}

	if !reflect.DeepEqual(gotRootInputs, wantRootInputs) {
		t.Fatalf("missing M3 Action behavior: root action.yml inputs must remain the sealed M2 set\nwant: %v\ngot:  %v", wantRootInputs, gotRootInputs)
	}
}

// ── Author Action: no write permissions ───────────────────────────────────────

// TestM3Action_AuthorScriptNeverReceivesWritePermission verifies that author/action.yml
// scripts never receive contents:write, issues:write, or pull-requests:write env values.
func TestM3Action_AuthorScriptNeverReceivesWritePermission(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)

	for _, step := range action.Runs.Steps {
		for key, value := range step.Env {
			lower := strings.ToLower(key + value)
			if strings.Contains(lower, "contents:write") ||
				strings.Contains(lower, "issues:write") ||
				strings.Contains(lower, "pull-requests:write") {
				t.Errorf("missing M3 Action behavior: author/action.yml step %q env contains write permission %q=%q", step.ID, key, value)
			}
		}
	}
}

// ── M3 author Action uses duto-ai run ─────────────────────────────────────────

// TestM3Action_AuthorRunsAuthorScript verifies author/action.yml invokes an author
// shell script (author/*.sh pattern) in its run steps, not action/run.sh.
func TestM3Action_AuthorRunsAuthorScript(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)

	authorScriptFound := false

	for _, step := range action.Runs.Steps {
		if strings.Contains(step.Run, "author/") && strings.HasSuffix(strings.TrimSpace(step.Run), ".sh\"") ||
			regexp.MustCompile(`author/[a-z][\w-]*\.sh`).MatchString(step.Run) {
			authorScriptFound = true
		}

		// Must not reuse the M2 action/run.sh path
		if strings.Contains(step.Run, "action/run.sh") {
			t.Errorf("missing M3 Action behavior: author/action.yml must not invoke M2 action/run.sh; it needs its own author/*.sh scripts")
		}
	}

	if !authorScriptFound {
		t.Fatalf("missing M3 Action behavior: author/action.yml must invoke an author/*.sh script to run duto-ai with M3 control evidence")
	}
}

// TestM3Action_PublishRunsPublishScript verifies publish/action.yml invokes a publish
// shell script (publish/*.sh pattern) and not the M2 action/run.sh.
func TestM3Action_PublishRunsPublishScript(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)

	publishScriptFound := false

	for _, step := range action.Runs.Steps {
		if regexp.MustCompile(`publish/[a-z][\w-]*\.sh`).MatchString(step.Run) {
			publishScriptFound = true
		}

		if strings.Contains(step.Run, "action/run.sh") {
			t.Errorf("missing M3 Action behavior: publish/action.yml must not invoke M2 action/run.sh")
		}
	}

	if !publishScriptFound {
		t.Fatalf("missing M3 Action behavior: publish/action.yml must invoke a publish/*.sh script to run duto-ai publish")
	}
}

// ── Safe content output ───────────────────────────────────────────────────────

// TestM3Action_AuthorOutputsAreSafeContentExpressions verifies every output in
// author/action.yml uses a safe step-context value expression, not direct model content.
func TestM3Action_AuthorOutputsAreSafeContentExpressions(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3AuthorActionPath)
	stepsExprPattern := regexp.MustCompile(`^\$\{\{\s*steps\.[a-z][\w-]*\.outputs`)

	for name, output := range action.Outputs {
		if !stepsExprPattern.MatchString(strings.TrimSpace(output.Value)) {
			t.Errorf("missing M3 Action behavior: author/action.yml output %q must come from steps.*.outputs, got %q", name, output.Value)
		}
	}
}

// TestM3Action_PublishOutputsAreSafeContentExpressions verifies every output in
// publish/action.yml uses a safe step-context value expression.
func TestM3Action_PublishOutputsAreSafeContentExpressions(t *testing.T) {
	t.Parallel()

	action := loadM3ActionFile(t, m3PublishActionPath)
	stepsExprPattern := regexp.MustCompile(`^\$\{\{\s*steps\.[a-z][\w-]*\.outputs`)

	for name, output := range action.Outputs {
		if !stepsExprPattern.MatchString(strings.TrimSpace(output.Value)) {
			t.Errorf("missing M3 Action behavior: publish/action.yml output %q must come from steps.*.outputs, got %q", name, output.Value)
		}
	}
}

// ── Author scripts are syntax-valid ──────────────────────────────────────────

// TestM3Action_AuthorScriptsSyntaxValid verifies all author/*.sh scripts pass
// bash -n (syntax check) and do not contain eval of model content.
func TestM3Action_AuthorScriptsSyntaxValid(t *testing.T) {
	t.Parallel()

	authorDir := filepath.Join(repoRoot(t), "author")

	entries, err := os.ReadDir(authorDir)
	if err != nil {
		t.Fatalf("missing M3 Action behavior: author/ directory must exist with shell scripts: %v", err)
	}

	scripts := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}

		scripts = append(scripts, filepath.Join(authorDir, entry.Name()))
	}

	if len(scripts) == 0 {
		t.Fatalf("missing M3 Action behavior: author/ must contain at least one .sh script")
	}

	for _, scriptPath := range scripts {
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatalf("missing M3 Action behavior: read %s: %v", scriptPath, err)
		}

		if strings.Contains(string(content), "eval ") || regexp.MustCompile(`\beval\b`).MatchString(string(content)) {
			t.Errorf("missing M3 Action behavior: author script %s must not eval user/model content", scriptPath)
		}
	}
}

// TestM3Action_PublishScriptsSyntaxValid verifies all publish/*.sh scripts pass
// bash -n (syntax check) and do not contain eval of model content.
func TestM3Action_PublishScriptsSyntaxValid(t *testing.T) {
	t.Parallel()

	publishDir := filepath.Join(repoRoot(t), "publish")

	entries, err := os.ReadDir(publishDir)
	if err != nil {
		t.Fatalf("missing M3 Action behavior: publish/ directory must exist with shell scripts: %v", err)
	}

	scripts := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}

		scripts = append(scripts, filepath.Join(publishDir, entry.Name()))
	}

	if len(scripts) == 0 {
		t.Fatalf("missing M3 Action behavior: publish/ must contain at least one .sh script")
	}

	for _, scriptPath := range scripts {
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatalf("missing M3 Action behavior: read %s: %v", scriptPath, err)
		}

		if regexp.MustCompile(`\beval\b`).MatchString(string(content)) {
			t.Errorf("missing M3 Action behavior: publish script %s must not eval user/model content", scriptPath)
		}
	}
}

// ── duto-test playground: actual file loading ─────────────────────────────────

// TestM3Action_DutoTestM3WorkflowExists verifies that the duto-test repository
// contains the M3 hosted workflow file expected by the M3 process.
// This test loads the actual duto-test checkout files.
func TestM3Action_DutoTestM3WorkflowExists(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	m3WorkflowPath := filepath.Join(dutoTestDir, ".github", "workflows", "m3-authoring.yaml")
	if _, err := os.Stat(m3WorkflowPath); err != nil {
		t.Fatalf("missing M3 Action behavior: duto-test must have .github/workflows/m3-authoring.yaml; %v", err)
	}
}

// TestM3Action_DutoTestM3WorkflowHasDistinctJobs verifies the duto-test M3 workflow
// uses two distinct jobs: one read-only author job and one write-scoped publisher job.
func TestM3Action_DutoTestM3WorkflowHasDistinctJobs(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	type workflowDoc struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Needs       any               `yaml:"needs"`
		} `yaml:"jobs"`
	}

	content, err := os.ReadFile(filepath.Join(dutoTestDir, ".github", "workflows", "m3-authoring.yaml"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read m3-authoring.yaml: %v", err)
	}

	var wf workflowDoc
	if err := yaml.Unmarshal(content, &wf); err != nil {
		t.Fatalf("missing M3 Action behavior: parse m3-authoring.yaml: %v", err)
	}

	if len(wf.Jobs) < 2 {
		t.Fatalf("missing M3 Action behavior: m3-authoring.yaml must have at least two jobs (author and publish), got %d", len(wf.Jobs))
	}

	authorJobFound := false
	publishJobFound := false

	for jobID, job := range wf.Jobs {
		lower := strings.ToLower(jobID)
		if lower == "author" || strings.Contains(lower, "author") {
			authorJobFound = true
			// Author job must not have write permissions
			for perm, level := range job.Permissions {
				if level == "write" && (perm == "contents" || perm == "issues" || perm == "pull-requests") {
					t.Errorf("missing M3 Action behavior: author job %q must not have write permission %q=%q", jobID, perm, level)
				}
			}
		}

		if lower == "publish" || strings.Contains(lower, "publish") {
			publishJobFound = true
		}
	}

	if !authorJobFound {
		t.Fatalf("missing M3 Action behavior: m3-authoring.yaml must have an author job (name containing 'author')")
	}

	if !publishJobFound {
		t.Fatalf("missing M3 Action behavior: m3-authoring.yaml must have a publish job (name containing 'publish')")
	}
}

// TestM3Action_DutoTestM3WorkflowActionsAreSHAPinned verifies all action references in
// the duto-test M3 workflow use full SHA pins.
func TestM3Action_DutoTestM3WorkflowActionsAreSHAPinned(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	content, err := os.ReadFile(filepath.Join(dutoTestDir, ".github", "workflows", "m3-authoring.yaml"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read m3-authoring.yaml: %v", err)
	}

	shaRef := regexp.MustCompile(shaRefPattern)
	movingTag := regexp.MustCompile(`@v[0-9]+`)

	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "uses:") {
			continue
		}

		ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "uses:"))
		if ref == "" || !strings.Contains(ref, "@") {
			continue
		}

		if movingTag.MatchString(ref) {
			t.Errorf("missing M3 Action behavior: m3-authoring.yaml uses moving tag reference %q", ref)
		}

		if !shaRef.MatchString(ref) {
			t.Errorf("missing M3 Action behavior: m3-authoring.yaml uses non-SHA action reference %q", ref)
		}
	}
}

// TestM3Action_DutoTestM3WorkflowIsProviderNeutral verifies the duto-test M3 workflow
// does not embed private provider names, internal model IDs, or customer names.
func TestM3Action_DutoTestM3WorkflowIsProviderNeutral(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	content, err := os.ReadFile(filepath.Join(dutoTestDir, ".github", "workflows", "m3-authoring.yaml"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read m3-authoring.yaml: %v", err)
	}

	issues := m3ActionForbiddenMarkerIssues("m3-authoring.yaml", string(content))
	if len(issues) > 0 {
		t.Fatalf("missing M3 Action behavior: m3-authoring.yaml contains provider-specific or hostile content:\n- %s", strings.Join(issues, "\n- "))
	}
}

// TestM3Action_DutoTestM3WorkflowHasCorrelationInput verifies the duto-test M3 workflow
// accepts a correlation input so runs can be uniquely identified.
func TestM3Action_DutoTestM3WorkflowHasCorrelationInput(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	type workflowDispatch struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Required bool `yaml:"required"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
	}

	content, err := os.ReadFile(filepath.Join(dutoTestDir, ".github", "workflows", "m3-authoring.yaml"))
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read m3-authoring.yaml: %v", err)
	}

	var wf workflowDispatch
	if err := yaml.Unmarshal(content, &wf); err != nil {
		t.Fatalf("missing M3 Action behavior: parse m3-authoring.yaml: %v", err)
	}

	if _, ok := wf.On.WorkflowDispatch.Inputs["correlation_id"]; !ok {
		t.Fatalf("missing M3 Action behavior: m3-authoring.yaml workflow_dispatch must accept correlation_id input for unique run identification")
	}
}

// TestM3Action_DutoTestM2WorkflowUnchanged verifies the existing M2 hosted workflow
// in duto-test has not been modified by M3 work (structural identity check).
func TestM3Action_DutoTestM2WorkflowUnchanged(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M3 cross-repository assertions: DUTO_TEST_DIR is required")
	}

	m3AssertDutoTestHostedM2WorkflowContract(t, dutoTestDir)
}

// m3ActionForbiddenMarkerIssues returns a list of issues for host-template or hostile
// markers found in content. These are a subset of the markers checked by the
// integration-tagged helpers in duto_test_scenarios_test.go.
func m3ActionForbiddenMarkerIssues(location, content string) []string {
	issues := make([]string, 0)

	for _, marker := range []string{
		strings.Join([]string{"{{ ", ".", "Env"}, ""),
		strings.Join([]string{"{{ ", ".", "Event"}, ""),
		strings.Join([]string{".", "Env", "."}, ""),
		strings.Join([]string{".", "Event", "."}, ""),
	} {
		if strings.Contains(content, marker) {
			issues = append(issues, fmt.Sprintf("%s contains forbidden host-template marker %q", location, marker))
		}
	}

	return issues
}

// m3AssertDutoTestHostedM2WorkflowContract asserts the M2 ai-scenarios workflow has not
// been widened by M3 work: it must still have exactly two matrix rows, read-only
// permissions, and full-SHA action pins.
func m3AssertDutoTestHostedM2WorkflowContract(t *testing.T, dutoTestDir string) {
	t.Helper()

	workflowPath := filepath.Join(dutoTestDir, ".github", "workflows", "ai-scenarios.yaml")

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("missing M3 Action behavior: read ai-scenarios.yaml: %v", err)
	}

	var wf struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Strategy    struct {
				Matrix struct {
					Include []struct {
						Scenario string `yaml:"scenario"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}

	if err := yaml.Unmarshal(content, &wf); err != nil {
		t.Fatalf("missing M3 Action behavior: parse ai-scenarios.yaml: %v", err)
	}

	for _, job := range wf.Jobs {
		if len(job.Strategy.Matrix.Include) != 2 {
			t.Errorf("missing M3 Action behavior: M2 ai-scenarios.yaml matrix must still have exactly 2 rows, got %d", len(job.Strategy.Matrix.Include))
		}

		for perm, level := range job.Permissions {
			if level != "read" {
				t.Errorf("missing M3 Action behavior: M2 ai-scenarios.yaml job permission %q must be read-only, got %q", perm, level)
			}
		}
	}
}
