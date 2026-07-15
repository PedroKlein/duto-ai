package prompt

import (
	"bytes"
	"os"
	"strings"
	"text/template"
)

// TemplateData is the data available to Go templates in prompt strings.
type TemplateData struct {
	Event EventTemplateData
	Env   map[string]string
}

// EventTemplateData exposes event context fields for template rendering.
type EventTemplateData struct {
	Owner       string
	Repo        string
	PRNumber    int
	IssueNumber int
	Author      string
	Event       string // event name (e.g. "pull_request")
}

// RenderTemplate renders a prompt string as a Go template with event context
// and environment variables. Template expressions referencing .Steps are
// stripped (ADK handles inter-step output passing natively).
func RenderTemplate(raw string, eventCtx *EventContext) (string, error) {
	// If no template delimiters present, return as-is (fast path).
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}

	data := buildTemplateData(eventCtx)

	tmpl, err := template.New("prompt").
		Option("missingkey=zero").
		Parse(raw)
	if err != nil {
		return raw, nil //nolint:nilerr // unparseable template treated as literal
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return raw, nil //nolint:nilerr // execution error treated as literal
	}

	return buf.String(), nil
}

func buildTemplateData(eventCtx *EventContext) TemplateData {
	data := TemplateData{
		Env: envMap(),
	}

	if eventCtx != nil {
		owner, repo, _ := splitRepo(eventCtx.Repo)

		data.Event = EventTemplateData{
			Owner:       owner,
			Repo:        repo,
			PRNumber:    eventCtx.PRNumber,
			IssueNumber: eventCtx.IssueNumber,
			Author:      eventCtx.Author,
			Event:       eventCtx.EventName,
		}
	}

	return data
}

func envMap() map[string]string {
	env := make(map[string]string)

	for _, kv := range os.Environ() {
		key, val, _ := strings.Cut(kv, "=")
		env[key] = val
	}

	return env
}
