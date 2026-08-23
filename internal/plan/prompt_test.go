package plan_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestCompile_FreezesInstructionAndSelectedSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "go-review", "references"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "review.tmpl"), []byte(`Review {{ quote .Step.Inputs.objective }}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "skills", "go-review", "SKILL.md"), []byte("---\nname: go-review\ndescription: Review Go code.\n---\nOriginal skill."), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "skills", "go-review", "references", "guide.md"), []byte("Original guide."), 0o600); err != nil {
		t.Fatal(err)
	}

	configYAML := runtimeConfig + fmt.Sprintf("workspaces:\n  source: {root: %q, access: read}\n", root)
	workflowYAML := strings.Replace(minimalWorkflow, "tools: []\nlimits:", "tools: []\nskills:\n  go-review: {workspace: source, path: skills/go-review}\nlimits:", 1)
	workflowYAML = strings.Replace(workflowYAML, "instruction: {text: Report the objective.}", "instruction: {template: {file: {workspace: source, path: review.tmpl, max_bytes: 256}, max_output_bytes: 256}}", 1)
	workflowYAML = strings.Replace(workflowYAML, "    tools: []\n    workspaces: []", "    tools: []\n    skills: [go-review]\n    workspaces: [{name: source, access: read}]", 1)
	cfg, workflow := decodeInputs(t, configYAML, workflowYAML)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "review.tmpl"), []byte("changed"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	snapshot := compiled.Snapshot()
	step := snapshot.Workflow.Steps[0]

	got, err := step.Instruction.Render(prompt.Data{Step: prompt.StepData{ID: "report", Inputs: map[string]any{"objective": "find bugs"}}})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if got != `Review "find bugs"` {
		t.Fatalf("Render() = %q", got)
	}

	if step.Instruction.Workspace != "source" || step.Instruction.Path != "review.tmpl" || step.Instruction.Digest == "" {
		t.Fatalf("instruction projection = %#v", step.Instruction)
	}

	if len(snapshot.Workflow.Skills) != 1 || snapshot.Workflow.Skills[0].Name != "go-review" || len(snapshot.Workflow.Skills[0].Files) != 2 {
		t.Fatalf("skills projection = %#v", snapshot.Workflow.Skills)
	}

	if strings.Contains(string(compiled.JSON()), root) {
		t.Fatal("plan projection leaked concrete trusted workspace root")
	}
}

func TestCompile_RejectsUnselectedOrUnadmittedSkillWorkspace(t *testing.T) {
	t.Parallel()

	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)

	workflow.Steps[0].Skills = []string{"unknown"}
	if _, err := plan.Compile(cfg, workflow); err == nil {
		t.Fatal("Compile() error = nil for unknown skill")
	}

	workflow.Skills = map[string]config.SkillSource{"unknown": {Workspace: "missing", Path: "skill"}}
	if _, err := plan.Compile(cfg, workflow); err == nil {
		t.Fatal("Compile() error = nil for unadmitted workspace")
	}
}
