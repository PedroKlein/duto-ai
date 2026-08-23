package plan_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const toolConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-model}
workspaces:
  source: {root: ., access: read}
tool_config:
  files: {workspace: source}
  git: {workspace: source, refs: [HEAD], allow_working_tree: true, max_log_count: 20}
  github: {base_url: https://api.example.test, owner: example-owner, repository: example-repository, subject: 1, ref: example-ref, max_pages: 2, max_results: 20}
  web: {allowed_domains: [example.test], max_redirects: 0}
  shell: {executable: /bin/echo, args: [], workspace: source, environment: {}, max_stdout_bytes: 256, max_stderr_bytes: 256}
tools: [files.*, git.read.*, github.read.*, web.fetch, shell.run]
tool_profiles:
  source-review: [files.grep, git.read.*]
tool_limits:
  files.find: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  files.grep: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  files.read: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  git.read.blame: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  git.read.diff: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  git.read.log: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  git.read.show: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.issue: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.pr: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.diff: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.changed-files: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.comments: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.reviews: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.checks: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  github.read.search-issues: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  web.fetch: {max_calls: 4, timeout: 10s, max_result_bytes: 1024}
  shell.run: {max_calls: 2, timeout: 5s, max_request_bytes: 256, max_result_bytes: 1024}
`

func TestCompile_ToolAuthorityProfilesAndLimits(t *testing.T) {
	workflowYAML := strings.Replace(minimalWorkflow,
		"tools: []\nlimits:",
		"tool_profiles:\n  focused: [files.read, github.read.pr]\ntools:\n  add_profiles: [source-review, focused]\n  add: [files.*]\n  remove_profiles: [focused]\n  remove: [git.read.blame]\ntool_limits:\n  files.grep: {max_calls: 2, timeout: 3s, max_result_bytes: 512}\nlimits:", 1)
	workflowYAML = strings.Replace(workflowYAML, "  max_tool_calls: 0", "  max_tool_calls: 10", 1)
	workflowYAML = strings.Replace(workflowYAML,
		"    tools: []\n    workspaces:",
		"    tools: {from: parent, remove: [git.read.log]}\n    tool_limits:\n      files.grep: {max_calls: 1, timeout: 2s}\n    workspaces:", 1)

	cfg, workflow := decodeInputs(t, toolConfig, workflowYAML)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	snapshot := compiled.Snapshot()

	wantWorkflow := []string{"files.find", "files.grep", "git.read.diff", "git.read.log", "git.read.show"}
	if diff := cmp.Diff(wantWorkflow, snapshot.Workflow.Tools.Names); diff != "" {
		t.Fatalf("workflow tools mismatch (-want +got):\n%s", diff)
	}

	wantStep := []string{"files.find", "files.grep", "git.read.diff", "git.read.show"}
	if diff := cmp.Diff(wantStep, snapshot.Workflow.Steps[0].Tools.Names); diff != "" {
		t.Fatalf("step tools mismatch (-want +got):\n%s", diff)
	}

	limits := snapshot.Workflow.Steps[0].Tools.Limits
	if len(limits) != 4 || limits[1].Name != "files.grep" || limits[1].MaxCalls != 1 || limits[1].Timeout != "2s" || limits[1].MaxResultBytes != 512 {
		t.Fatalf("step tool limits = %#v", limits)
	}

	if snapshot.Workflow.CatalogDigest == "" || len(snapshot.Workflow.Tools.Definitions) != len(wantWorkflow) {
		t.Fatalf("catalog projection = %#v", snapshot.Workflow.Tools)
	}
}

func TestCompile_RejectsConcurrentProcessAuthority(t *testing.T) {
	cfg, workflow := decodeInputs(t, toolConfig, minimalWorkflow)
	workflow.Limits.MaxToolCalls = 10
	workflow.Tools = config.ToolExpression{Add: []string{"files.read", "shell.run"}}
	workflow.Steps[0].Tools = config.ToolExpression{From: dtool.FromParent}
	workflow.Steps[0].Limits.MaxParallelCalls = 2

	_, err := plan.Compile(cfg, workflow)
	if !errors.Is(err, plan.ErrInvalidLimits) {
		t.Fatalf("parallel process calls error = %v", err)
	}

	workflow.Steps[0].Limits.MaxParallelCalls = 1
	other := workflow.Steps[0]
	other.ID = "other"
	workflow.Steps = append(workflow.Steps, other)

	_, err = plan.Compile(cfg, workflow)
	if !errors.Is(err, plan.ErrInvalidLimits) {
		t.Fatalf("overlapping process steps error = %v", err)
	}
}

func TestCompile_ToolAdmissionStableErrors(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		tools      string
		code       string
	}{
		{name: "unknown tool", configYAML: toolConfig, tools: "[files.missing]", code: dtool.CodeUnknownTool},
		{name: "broad wildcard", configYAML: toolConfig, tools: "[github.*]", code: dtool.CodeInvalidToolSelector},
		{name: "ceiling", configYAML: strings.Replace(toolConfig, "tools: [files.*, git.read.*, github.read.*, web.fetch, shell.run]", "tools: [files.*]", 1), tools: "[web.fetch]", code: dtool.CodeToolCeilingExceeded},
		{name: "parent", configYAML: toolConfig, tools: "[files.read]", code: dtool.CodeToolAuthorityWidening},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workflowYAML := strings.Replace(minimalWorkflow, "tools: []", "tools: "+test.tools, 1)

			workflowYAML = strings.Replace(workflowYAML, "  max_tool_calls: 0", "  max_tool_calls: 10", 1)
			if test.name == "parent" {
				workflowYAML = strings.Replace(workflowYAML, "    tools: []", "    tools: [files.grep]", 1)
			}

			cfg, workflow := decodeInputs(t, test.configYAML, workflowYAML)
			_, compileErr := plan.Compile(cfg, workflow)

			var policyErr *dtool.PolicyError
			if !errors.As(compileErr, &policyErr) || policyErr.Code != test.code {
				t.Fatalf("Compile() error = %v, want code %q", compileErr, test.code)
			}
		})
	}
}
