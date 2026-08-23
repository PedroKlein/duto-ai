package plan_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/plan"
)

const delegatedConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-light}
  capable: {provider: default, target: example-capable}
tool_config:
  github: {base_url: https://api.example.test, owner: example-owner, repository: example-repository, subject: 1, ref: example-ref, max_pages: 2, max_results: 20}
  web: {allowed_domains: [example.test], max_redirects: 0}
`

func TestCompile_NativeAgentPlanIsFiniteAndDeterministic(t *testing.T) {
	cfg, workflow := decodeInputs(t, delegatedConfig, delegatedWorkflowYAML)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	snapshot := compiled.Snapshot()
	if len(snapshot.Workflow.Agents) != 2 || snapshot.Workflow.Agents[0].Name != "researcher" || snapshot.Workflow.Agents[1].Name != "coordinator" {
		t.Fatalf("agents = %#v", snapshot.Workflow.Agents)
	}

	researcher := snapshot.Workflow.Agents[0]
	if researcher.Mode != "single_turn" || researcher.Context.Mode != "snapshot" || len(researcher.Context.Include) != 1 || researcher.Context.Include[0].Kind != "input" {
		t.Fatalf("researcher = %#v", researcher)
	}

	coordinator := snapshot.Workflow.Agents[1]
	if len(coordinator.Subagents) != 1 || coordinator.Subagents[0] != "researcher" || snapshot.Workflow.Steps[0].Agent != "coordinator" {
		t.Fatalf("coordinator/step = %#v/%#v", coordinator, snapshot.Workflow.Steps[0])
	}

	if got := strings.Join(snapshot.Models, ","); got != "capable,light" {
		t.Fatalf("models = %q", got)
	}
}

func TestCompile_SubagentToolScopesRemainExactAndIndependent(t *testing.T) {
	cfgYAML := strings.Replace(delegatedConfig, "models:", `tools: [web.fetch, github.read.pr]
tool_limits:
  web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
  github.read.pr: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
models:`, 1)
	workflowYAML := strings.Replace(delegatedWorkflowYAML, "tools: []\nlimits:", `tools: [web.fetch, github.read.pr]
tool_limits:
  web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
  github.read.pr: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
limits:`, 1)
	workflowYAML = strings.Replace(workflowYAML, "    tools: []\n    workspaces: []\n    skills: []\n    context:\n      mode: snapshot", `    tools: [web.fetch]
    tool_limits:
      web.fetch: {max_calls: 1, timeout: 1s, max_request_bytes: 64, max_result_bytes: 256}
    workspaces: []
    skills: []
    context:
      mode: snapshot`, 1)
	workflowYAML = strings.Replace(workflowYAML, "    tools: []\n    workspaces: []\n    skills: []\n    context: {mode: fresh}", `    tools: [web.fetch, github.read.pr]
    tool_limits:
      web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
      github.read.pr: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}
    workspaces: []
    skills: []
    context: {mode: fresh}`, 1)
	cfg, workflow := decodeInputs(t, cfgYAML, workflowYAML)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	agents := compiled.Snapshot().Workflow.Agents
	if got := strings.Join(agents[0].Tools.Names, ","); got != "web.fetch" {
		t.Fatalf("researcher tools = %q", got)
	}

	if got := strings.Join(agents[1].Tools.Names, ","); got != "github.read.pr,web.fetch" {
		t.Fatalf("coordinator tools = %q", got)
	}
}

func TestCompile_RejectsStaticNodeDelegationBeforeConstruction(t *testing.T) {
	cfg, workflow := decodeInputs(t, delegatedConfig, strings.Replace(delegatedWorkflowYAML, "mode: chat", "mode: single_turn", 1))

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidAgent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidAgent", err)
	}
}

func TestCompile_RejectsSnapshotAncestorSourceUnavailableToNativeRootChat(t *testing.T) {
	workflowYAML := strings.Replace(delegatedWorkflowYAML, "- input: objective", "- output: {step: inspect, path: [findings]}", 1)
	cfg, workflow := decodeInputs(t, delegatedConfig, workflowYAML)

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidAgent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidAgent", err)
	}
}

func TestCompile_RejectsSubagentDepthAboveWorkflowLimit(t *testing.T) {
	workflowYAML := strings.Replace(delegatedWorkflowYAML, "max_iterations: 8", "max_iterations: 1", 1)
	cfg, workflow := decodeInputs(t, delegatedConfig, workflowYAML)

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidAgent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidAgent", err)
	}
}

func TestCompile_RejectsSnapshotBoundBelowDeclaredSource(t *testing.T) {
	workflowYAML := strings.Replace(delegatedWorkflowYAML, "max_artifact_bytes: 1024", "max_artifact_bytes: 16", 1)
	cfg, workflow := decodeInputs(t, delegatedConfig, workflowYAML)

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidAgent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidAgent", err)
	}
}

func TestCompile_RejectsDelegationAuthorityWideningBeforeConstruction(t *testing.T) {
	cfgYAML := strings.Replace(delegatedConfig, "models:", "tools: [web.fetch]\ntool_limits:\n  web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}\nmodels:", 1)
	workflowYAML := strings.Replace(delegatedWorkflowYAML, "tools: []\nlimits:", "tools: [web.fetch]\ntool_limits:\n  web.fetch: {max_calls: 2, timeout: 2s, max_request_bytes: 128, max_result_bytes: 512}\nlimits:", 1)
	workflowYAML = strings.Replace(workflowYAML, "    tools: []\n    workspaces: []\n    skills: []\n    context: {mode: fresh}", "    tools: []\n    workspaces: []\n    skills: []\n    context: {mode: fresh}", 1)
	workflowYAML = strings.Replace(workflowYAML, "    tools: []\n    workspaces: []\n    skills: []\n    context:\n      mode: snapshot", "    tools: [web.fetch]\n    tool_limits:\n      web.fetch: {max_calls: 1, timeout: 1s, max_request_bytes: 64, max_result_bytes: 256}\n    workspaces: []\n    skills: []\n    context:\n      mode: snapshot", 1)

	cfg, workflow := decodeInputs(t, cfgYAML, workflowYAML)

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidAgent) {
		t.Fatalf("Compile() error = %v, want ErrInvalidAgent", err)
	}
}

const delegatedWorkflowYAML = `version: 1
name: delegated
inputs:
  objective: {schema: {type: string, max_length: 64}}
model: capable
tools: []
limits: {timeout: 1m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 2, max_artifact_bytes: 1024}
agents:
  researcher:
    description: Gather evidence.
    mode: single_turn
    model: light
    instruction: {text: Return evidence.}
    tools: []
    workspaces: []
    skills: []
    context:
      mode: snapshot
      include:
        - input: objective
    input:
      type: object
      properties: {question: {type: string, max_length: 64}}
      required: [question]
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}
      required: [outcome, evidence]
  coordinator:
    description: Coordinate research.
    mode: chat
    model: capable
    instruction: {text: "Delegate once, then decide."}
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher]
    input:
      type: object
      properties: {objective: {type: string, max_length: 64}}
      required: [objective]
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, decision: {type: string, max_length: 128}}
      required: [outcome, decision]
steps:
  - id: coordinate
    needs: []
    agent: coordinator
    with: {objective: {input: objective}}
result: {step: coordinate}
`
