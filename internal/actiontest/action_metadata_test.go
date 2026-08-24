package actiontest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type actionMetadataFile struct {
	Inputs  map[string]actionInput  `yaml:"inputs"`
	Outputs map[string]actionOutput `yaml:"outputs"`
	Runs    actionRuns              `yaml:"runs"`
}

type actionInput struct {
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

type actionOutput struct {
	Value string `yaml:"value"`
}

type actionRuns struct {
	Using string       `yaml:"using"`
	Steps []actionStep `yaml:"steps"`
}

type actionStep struct {
	ID              string `yaml:"id"`
	If              string `yaml:"if"`
	Run             string `yaml:"run"`
	Uses            string `yaml:"uses"`
	ContinueOnError bool   `yaml:"continue-on-error"`
}

func TestAction_Metadata(t *testing.T) {
	action := loadActionMetadata(t)

	if action.Runs.Using != "composite" {
		t.Fatalf("missing action metadata behavior: runs.using must be composite, got %q", action.Runs.Using)
	}

	inputKeys := sortedActionKeys(action.Inputs)
	wantInputs := []string{"config", "evidence-retention-days", "version", "workflow"}

	if !reflect.DeepEqual(inputKeys, wantInputs) {
		t.Fatalf("missing action metadata behavior: inputs must be closed and exact\nwant: %v\ngot:  %v", wantInputs, inputKeys)
	}

	if !action.Inputs["workflow"].Required {
		t.Fatalf("missing action metadata behavior: workflow input must be required")
	}

	if action.Inputs["config"].Required || action.Inputs["config"].Default != "duto.yaml" {
		t.Fatalf("missing action metadata behavior: config input must be optional with default duto.yaml")
	}

	if !action.Inputs["version"].Required {
		t.Fatalf("missing action metadata behavior: version input must be required")
	}

	if action.Inputs["evidence-retention-days"].Required || action.Inputs["evidence-retention-days"].Default != "7" {
		t.Fatalf("missing action metadata behavior: evidence-retention-days input must be optional with default 7")
	}

	outputKeys := sortedActionKeys(action.Outputs)
	wantOutputs := []string{"clarification-required", "evidence-path", "failed-step", "outcome", "result-path", "run-id", "status"}

	if !reflect.DeepEqual(outputKeys, wantOutputs) {
		t.Fatalf("missing action metadata behavior: outputs must be closed and exact\nwant: %v\ngot:  %v", wantOutputs, outputKeys)
	}

	if action.Outputs["status"].Value != "${{ steps.project.outputs.status }}" {
		t.Fatalf("missing action metadata behavior: status output must come from project step")
	}

	if action.Outputs["clarification-required"].Value != "${{ steps.project.outputs['clarification-required'] }}" {
		t.Fatalf("missing action metadata behavior: clarification-required output must come from project step")
	}

	forbiddenInputs := []string{"latest", "log-level", "output-file", "output-format", "verbose"}

	for _, key := range forbiddenInputs {
		if _, ok := action.Inputs[key]; ok {
			t.Fatalf("missing action metadata behavior: forbidden legacy input %q must be absent", key)
		}
	}
}

func TestAction_Composition(t *testing.T) {
	action := loadActionMetadata(t)

	if len(action.Runs.Steps) != 6 {
		t.Fatalf("missing action composition behavior: expected exactly 6 steps, got %d", len(action.Runs.Steps))
	}

	gotOrder := make([]string, 0, len(action.Runs.Steps))
	for _, step := range action.Runs.Steps {
		gotOrder = append(gotOrder, step.ID)
	}

	wantOrder := []string{"install", "prepare", "run", "project", "upload", "final-exit"}

	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("missing action composition behavior: step ids must follow install→prepare→run→project→upload→final-exit\nwant: %v\ngot:  %v", wantOrder, gotOrder)
	}

	install := action.Runs.Steps[0]
	if !strings.Contains(install.Run, "action/install.sh") {
		t.Fatalf("missing action composition behavior: install step must execute action/install.sh")
	}

	prepare := action.Runs.Steps[1]
	if !strings.Contains(prepare.Run, "action/prepare.sh") {
		t.Fatalf("missing action composition behavior: prepare step must execute action/prepare.sh")
	}

	run := action.Runs.Steps[2]
	if !run.ContinueOnError {
		t.Fatalf("missing action composition behavior: run step must preserve exit for final step")
	}

	if !strings.Contains(run.Run, "action/run.sh") {
		t.Fatalf("missing action composition behavior: run step must execute action/run.sh")
	}

	project := action.Runs.Steps[3]
	if project.If != "always()" || !strings.Contains(project.Run, "action/project.sh") {
		t.Fatalf("missing action composition behavior: project step must run always() and execute action/project.sh")
	}

	upload := action.Runs.Steps[4]
	if upload.If != "always()" {
		t.Fatalf("missing action composition behavior: upload step must run always()")
	}

	if upload.Uses != "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02" {
		t.Fatalf("missing action composition behavior: upload step must use pinned actions/upload-artifact SHA")
	}

	finalExit := action.Runs.Steps[5]
	if finalExit.If != "always()" {
		t.Fatalf("missing action composition behavior: final exit step must run always()")
	}

	if !strings.Contains(finalExit.Run, "DUTO_ACTION_RUN_EXIT_CODE") {
		t.Fatalf("missing action composition behavior: final exit step must restore DUTO_ACTION_RUN_EXIT_CODE")
	}
}

func loadActionMetadata(t *testing.T) actionMetadataFile {
	t.Helper()

	path := filepath.Join(repoRoot(t), "action.yml")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("action metadata setup failure: read action.yml: %v", err)
	}

	var action actionMetadataFile
	if err := yaml.Unmarshal(content, &action); err != nil {
		t.Fatalf("action metadata setup failure: parse action.yml: %v", err)
	}

	return action
}

func sortedActionKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
