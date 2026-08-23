package main

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

func TestBuildToolRegistry_ConstructsOnlySelectedTrustedFamilies(t *testing.T) {
	root := t.TempDir()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cfg := decodeToolConfig(t, fmt.Sprintf(`version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  light: {provider: default, target: example-model}
workspaces:
  source: {root: %q, access: read}
tools: [files.*, git.read.*, github.read.*, web.fetch, shell.run]
tool_limits:
  files.find: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  files.grep: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  files.read: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  git.read.blame: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  git.read.diff: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  git.read.log: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  git.read.show: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 1024}
  github.read.changed-files: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.checks: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.comments: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.diff: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.issue: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.pr: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.reviews: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  github.read.search-issues: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
  shell.run: {max_calls: 1, timeout: 2s, max_request_bytes: 64, max_result_bytes: 1024}
  web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 1024}
tool_config:
  files: {workspace: source}
  git: {workspace: source, refs: [HEAD], allow_working_tree: true, max_log_count: 20}
  github: {base_url: https://api.example.test, owner: example-owner, repository: example-repository, subject: 7, ref: example-ref, max_pages: 2, max_results: 20}
  web: {allowed_domains: [example.test], max_redirects: 1}
  shell: {executable: %q, args: [], workspace: source, environment: {}, max_stdout_bytes: 256, max_stderr_bytes: 256}
`, root, executable))

	workflow := decodeToolWorkflow(t, `version: 1
name: all-families
model: light
tools: [files.read, git.read.log, github.read.pr, web.fetch, shell.run]
limits: {timeout: 30s, max_iterations: 2, max_model_calls: 2, max_tool_calls: 10, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: inspect
    needs: []
    instruction: Inspect using the admitted tools.
    tools: {from: parent}
    workspaces: [{name: source, access: read}]
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}}
      required: [outcome]
result: {step: inspect}
`)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	registry, err := buildToolRegistry(cfg, compiled)
	if err != nil {
		t.Fatalf("buildToolRegistry() error = %v", err)
	}

	want := []string{"files.read", "git.read.log", "github.read.pr", "shell.run", "web.fetch"}
	if got := registry.Names(); !slices.Equal(got, want) {
		t.Fatalf("registered tools = %v, want %v", got, want)
	}
}

func TestAdmissionRejectsMissingSelectedToolBinding(t *testing.T) {
	cfg := decodeToolConfig(t, `version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  light: {provider: default, target: example-model}
tools: [web.fetch]
tool_limits:
  web.fetch: {max_calls: 1, timeout: 2s, max_request_bytes: 128, max_result_bytes: 256}
`)
	workflow := decodeToolWorkflow(t, `version: 1
name: missing-binding
model: light
tools: [web.fetch]
limits: {timeout: 30s, max_iterations: 2, max_model_calls: 2, max_tool_calls: 1, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: inspect
    needs: []
    instruction: Inspect.
    tools: {from: parent}
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}}
      required: [outcome]
result: {step: inspect}
`)

	if _, err := plan.Compile(cfg, workflow); err == nil {
		t.Fatal("plan.Compile() error = nil for selected family without trusted binding")
	}
}

func decodeToolConfig(t *testing.T, source string) *config.Config {
	t.Helper()

	cfg, err := config.DecodeConfig("duto.yaml", []byte(source))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	return cfg
}

func decodeToolWorkflow(t *testing.T, source string) *config.Workflow {
	t.Helper()

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(source))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	return workflow
}
