package prompt

import (
	"fmt"
	"os"
	"strings"

	"github.com/PedroKlein/duto-ai/internal/config"
)

// BuildSystemPrompt assembles the system prompt from 5 layers:
// 1. Step metadata (name, available tools)
// 2. Event context (PR info)
// 3. Context files content
// 4. Skills content
// 5. User system: field
func BuildSystemPrompt(step config.Step, cfg *config.Config, eventCtx *EventContext) string {
	var parts []string

	// Layer 1: Step metadata
	parts = append(parts, buildMetadataLayer(step, cfg))

	// Layer 2: Event context
	if eventCtx != nil {
		parts = append(parts, buildEventLayer(eventCtx))
	}

	// Layer 3: Context files
	if cfg != nil {
		parts = append(parts, buildContextFilesLayer(cfg.ContextFiles))
	}

	// Layer 4: Skills
	parts = append(parts, buildSkillsLayer(step.Skills))

	// Layer 5: User system field
	if step.System != "" {
		parts = append(parts, step.System)
	}

	return strings.Join(filterEmpty(parts), "\n\n")
}

func buildMetadataLayer(step config.Step, cfg *config.Config) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "You are executing step %q in a workflow.", step.ID)

	if step.Output != "" {
		fmt.Fprintf(&sb, " Your output key is %q.", step.Output)
	}

	// Show available tools
	toolNames := resolveToolNames(step, cfg)
	if len(toolNames) > 0 {
		fmt.Fprintf(&sb, "\n\nAvailable tools: %s", strings.Join(toolNames, ", "))
	}

	return sb.String()
}

func buildEventLayer(ctx *EventContext) string {
	if ctx == nil {
		return ""
	}

	var parts []string

	if ctx.Repo != "" {
		parts = append(parts, "Repository: "+ctx.Repo)

		// Split owner/repo for tool calls
		if owner, repo, ok := splitRepo(ctx.Repo); ok {
			parts = append(parts, "Owner: "+owner, "Repo: "+repo)
		}
	}

	if ctx.PRNumber > 0 {
		parts = append(parts, fmt.Sprintf("PR Number: %d", ctx.PRNumber))
	}

	if ctx.IssueNumber > 0 {
		parts = append(parts, fmt.Sprintf("Issue Number: %d", ctx.IssueNumber))
	}

	if ctx.Author != "" {
		parts = append(parts, "Author: "+ctx.Author)
	}

	if ctx.EventName != "" {
		parts = append(parts, "Event: "+ctx.EventName)
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Event Context\nUse these values when calling tools:\n" + strings.Join(parts, "\n")
}

func splitRepo(fullRepo string) (owner, repo string, ok bool) {
	owner, repo, ok = strings.Cut(fullRepo, "/")

	return owner, repo, ok
}

func buildContextFilesLayer(files []string) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("## Project Context\n")

	for _, file := range files {
		content, err := os.ReadFile(file) //nolint:gosec // user-provided context files
		if err != nil {
			continue
		}

		fmt.Fprintf(&sb, "\n### %s\n%s\n", file, strings.TrimSpace(string(content)))
	}

	return sb.String()
}

// skillsRegistry is the global auto-discovered skills registry.
var skillsRegistry = NewSkillsRegistry()

func buildSkillsLayer(skills []string) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("## Skills\n")

	for _, skill := range skills {
		path := skillsRegistry.Resolve(skill)

		content, err := os.ReadFile(path) //nolint:gosec // resolved skill path
		if err != nil {
			continue
		}

		fmt.Fprintf(&sb, "\n### %s\n%s\n", skill, strings.TrimSpace(string(content)))
	}

	result := sb.String()
	if result == "## Skills\n" {
		return ""
	}

	return result
}

func resolveToolNames(step config.Step, cfg *config.Config) []string {
	if cfg == nil {
		if step.Tools == nil {
			return nil
		}

		return *step.Tools
	}

	var defaults []string
	if cfg.Defaults.Tools != nil {
		defaults = cfg.Defaults.Tools
	}

	resolved := make([]string, 0)

	if step.Tools == nil {
		resolved = append(resolved, defaults...)
	} else if len(*step.Tools) > 0 {
		resolved = append(resolved, defaults...)
		resolved = append(resolved, *step.Tools...)
	}

	return resolved
}

func filterEmpty(parts []string) []string {
	result := make([]string, 0, len(parts))

	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			result = append(result, p)
		}
	}

	return result
}
