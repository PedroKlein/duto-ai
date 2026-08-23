package prompt_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/skilltoolset"
	adkskill "google.golang.org/adk/v2/tool/skilltoolset/skill"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestAdmit_FileFreezesBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path := filepath.Join(root, "review.md")
	if err := os.WriteFile(path, []byte("review the original"), 0o600); err != nil {
		t.Fatal(err)
	}

	frozen, err := prompt.Admit(prompt.Source{
		Kind: prompt.KindFile,
		File: prompt.FileSource{Workspace: "source", Path: "review.md", MaxBytes: 64},
	}, map[string]string{"source": root})
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	if writeErr := os.WriteFile(path, []byte("changed after admission"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	got, err := frozen.Render(prompt.Data{})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if got != "review the original" {
		t.Fatalf("Render() = %q, want frozen original", got)
	}

	if frozen.Digest == "" || frozen.Workspace != "source" || frozen.Path != "review.md" {
		t.Fatalf("Frozen = %#v, want symbolic source and digest", frozen)
	}
}

func TestAdmit_TemplateUsesClosedDataAndFunctions(t *testing.T) {
	t.Parallel()

	frozen, err := prompt.Admit(prompt.Source{
		Kind:           prompt.KindTemplate,
		Text:           `{{ .Workflow.Name }}: {{ quote .Step.Inputs.objective }} {{ json .Predecessors }}`,
		MaxOutputBytes: 256,
	}, nil)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	got, err := frozen.Render(prompt.Data{
		Workflow:     prompt.WorkflowData{Name: "review", Inputs: map[string]any{}},
		Step:         prompt.StepData{ID: "inspect", Inputs: map[string]any{"objective": "find bugs"}},
		Predecessors: map[string]any{"gather": map[string]any{"outcome": "completed"}},
		Runtime:      prompt.RuntimeData{RunID: "opaque", Attempt: 1},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if got != `review: "find bugs" {"gather":{"outcome":"completed"}}` {
		t.Fatalf("Render() = %q", got)
	}

	for _, source := range []string{
		`{{ call .Step.Inputs.fn }}`,
		`{{ env "SECRET" }}`,
		`{{ define "loop" }}{{ template "loop" . }}{{ end }}{{ template "loop" . }}`,
	} {
		_, err := prompt.Admit(prompt.Source{Kind: prompt.KindTemplate, Text: source, MaxOutputBytes: 64}, nil)
		if err == nil {
			t.Fatalf("Admit(%q) error = nil, want rejection", source)
		}
	}
}

func TestAdmit_TemplateBounds(t *testing.T) {
	t.Parallel()

	tooManyActions := strings.Repeat(`{{ print "x" }}`, 129)

	tooDeep := strings.Repeat(`{{ if true }}`, 33) + strings.Repeat(`{{ end }}`, 33)
	for _, source := range []string{tooManyActions, tooDeep, `{{ html .Step.ID }}`, `{{ js .Step.ID }}`, `{{ urlquery .Step.ID }}`} {
		if _, err := prompt.Admit(prompt.Source{Kind: prompt.KindTemplate, Text: source, MaxOutputBytes: 256}, nil); err == nil {
			t.Fatalf("Admit() error = nil for bounded/forbidden template %q", source)
		}
	}

	frozen, err := prompt.Admit(prompt.Source{
		Kind:           prompt.KindTemplate,
		Text:           `{{ range .Step.Inputs.values }}{{ end }}`,
		MaxOutputBytes: 256 << 10,
	}, nil)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	values := make([]any, 10_001)
	if _, err := frozen.Render(prompt.Data{Step: prompt.StepData{Inputs: map[string]any{"values": values}}}); err == nil {
		t.Fatal("Render() error = nil after operation budget exhaustion")
	}
}

func TestAdmit_TemplateFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		data prompt.Data
		max  int
	}{
		{name: "missing key", text: `{{ .Step.Inputs.missing }}`, data: prompt.Data{Step: prompt.StepData{Inputs: map[string]any{}}}, max: 64},
		{name: "wrong index", text: `{{ index .Step.Inputs.values 4 }}`, data: prompt.Data{Step: prompt.StepData{Inputs: map[string]any{"values": []any{"one"}}}}, max: 64},
		{name: "render overflow", text: `{{ .Step.Inputs.value }}`, data: prompt.Data{Step: prompt.StepData{Inputs: map[string]any{"value": strings.Repeat("x", 65)}}}, max: 64},
		{name: "complex value needs json", text: `{{ .Step.Inputs.value }}`, data: prompt.Data{Step: prompt.StepData{Inputs: map[string]any{"value": []any{"one"}}}}, max: 64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			frozen, err := prompt.Admit(prompt.Source{Kind: prompt.KindTemplate, Text: test.text, MaxOutputBytes: test.max}, nil)
			if err != nil {
				t.Fatalf("Admit() error = %v", err)
			}

			if _, err := frozen.Render(test.data); err == nil {
				t.Fatal("Render() error = nil, want fail-closed error")
			}
		})
	}
}

func TestAdmit_FileRejectsUnsafeSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(root, "escape.md")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "invalid.md"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "large.md"), []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"../outside.md", "escape.md", "invalid.md", "large.md", "directory"} {
		_, err := prompt.Admit(prompt.Source{
			Kind: prompt.KindFile,
			File: prompt.FileSource{Workspace: "source", Path: path, MaxBytes: 4},
		}, map[string]string{"source": root})
		if err == nil {
			t.Fatalf("Admit(%q) error = nil, want rejection", path)
		}
	}
}

func TestFreezeSkills_RejectsTraversalAndOversizedResources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, root, "go-review", "files.read", "guidance", strings.Repeat("x", (256<<10)+1))

	for _, request := range []prompt.SkillRequest{
		{Workspace: "source", Path: "../go-review"},
		{Workspace: "source", Path: "go-review"},
	} {
		if _, err := prompt.FreezeSkills(map[string]prompt.SkillRequest{"go-review": request}, map[string]string{"source": root}); err == nil {
			t.Fatalf("FreezeSkills(%#v) error = nil", request)
		}
	}
}

func TestFreezeSkills_ExposesOnlyFrozenSelection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, root, "go-review", "shell.run", "original guidance", "original resource")
	writeSkill(t, root, "other", "web.fetch", "other guidance", "other resource")

	frozen, err := prompt.FreezeSkills(map[string]prompt.SkillRequest{
		"go-review": {Workspace: "source", Path: "go-review"},
	}, map[string]string{"source": root})
	if err != nil {
		t.Fatalf("FreezeSkills() error = %v", err)
	}

	writeSkill(t, root, "go-review", "shell.run", "changed guidance", "changed resource")

	source := prompt.NewSkillSource(frozen, []string{"go-review"})

	frontmatters, err := source.ListFrontmatters(t.Context())
	if err != nil {
		t.Fatalf("ListFrontmatters() error = %v", err)
	}

	if len(frontmatters) != 1 || frontmatters[0].Name != "go-review" || frontmatters[0].AllowedTools[0] != "shell.run" {
		t.Fatalf("ListFrontmatters() = %#v", frontmatters)
	}

	instructions, err := source.LoadInstructions(t.Context(), "go-review")
	if err != nil {
		t.Fatalf("LoadInstructions() error = %v", err)
	}

	if !strings.Contains(instructions, "original guidance") || strings.Contains(instructions, "changed guidance") {
		t.Fatalf("LoadInstructions() = %q, want frozen bytes", instructions)
	}

	if _, loadErr := source.LoadInstructions(t.Context(), "other"); !errors.Is(loadErr, adkskill.ErrSkillNotFound) {
		t.Fatalf("LoadInstructions(other) error = %v", loadErr)
	}

	resource, err := source.LoadResource(t.Context(), "go-review", "references/guide.md")
	if err != nil {
		t.Fatalf("LoadResource() error = %v", err)
	}
	defer resource.Close()

	body, err := io.ReadAll(resource)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "original resource" {
		t.Fatalf("resource = %q", body)
	}

	toolset, err := skilltoolset.New(context.Background(), skilltoolset.Config{Source: source})
	if err != nil {
		t.Fatalf("skilltoolset.New() error = %v", err)
	}

	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}

	for _, tool := range tools {
		if tool.Name() == "shell.run" {
			t.Fatal("skill allowed-tools widened executable authority")
		}
	}

	req := &model.LLMRequest{}
	if err := toolset.ProcessRequest(nil, req); err != nil {
		t.Fatalf("ProcessRequest() error = %v", err)
	}

	if req.Config == nil || req.Config.SystemInstruction == nil {
		t.Fatalf("ProcessRequest() instruction = %#v", req.Config)
	}

	var systemInstruction strings.Builder
	for _, part := range req.Config.SystemInstruction.Parts {
		systemInstruction.WriteString(part.Text)
	}

	if !strings.Contains(systemInstruction.String(), "go-review") || strings.Contains(systemInstruction.String(), "<name>\nother\n</name>") {
		t.Fatalf("ProcessRequest() instruction = %q", systemInstruction.String())
	}
}

func writeSkill(t *testing.T, root, name, allowedTool, instruction, resource string) {
	t.Helper()

	directory := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(directory, "references"), 0o700); err != nil {
		t.Fatal(err)
	}

	content := "---\nname: " + name + "\ndescription: Review Go code.\nallowed-tools: [" + allowedTool + "]\n---\n" + instruction
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(directory, "references", "guide.md"), []byte(resource), 0o600); err != nil {
		t.Fatal(err)
	}
}
