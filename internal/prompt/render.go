package prompt

import (
	"fmt"
	"os"
	"strings"
	"text/template"
)

// StepOutput holds the output of a completed step.
type StepOutput struct {
	Output string
}

// TemplateData is passed to Go templates for prompt rendering.
type TemplateData struct {
	Steps map[string]StepOutput
}

// RenderPrompt renders a prompt string as a Go template with the given data.
// If tmpl ends with .md, it is treated as a file path and loaded first.
func RenderPrompt(tmpl string, data TemplateData) (string, error) {
	content := tmpl

	if strings.HasSuffix(tmpl, ".md") {
		fileContent, err := os.ReadFile(tmpl) //nolint:gosec // user-provided prompt path
		if err != nil {
			return "", fmt.Errorf("reading prompt file %s: %w", tmpl, err)
		}

		content = string(fileContent)
	}

	t, err := template.New("prompt").Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
