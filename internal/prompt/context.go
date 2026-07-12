package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// EventContext holds GitHub Actions event data extracted from environment.
type EventContext struct {
	Repo      string
	PRNumber  int
	Author    string
	EventName string
	EventPath string
}

// LoadEventContext reads event context from GHA environment variables.
func LoadEventContext() (*EventContext, error) {
	ctx := &EventContext{
		Repo:      os.Getenv("GITHUB_REPOSITORY"),
		EventName: os.Getenv("GITHUB_EVENT_NAME"),
		EventPath: os.Getenv("GITHUB_EVENT_PATH"),
	}

	if ctx.EventPath != "" {
		if err := ctx.parseEventFile(); err != nil {
			return ctx, fmt.Errorf("parsing event file: %w", err)
		}
	}

	return ctx, nil
}

func (ctx *EventContext) parseEventFile() error {
	data, err := os.ReadFile(ctx.EventPath)
	if err != nil {
		return fmt.Errorf("reading event file %s: %w", ctx.EventPath, err)
	}

	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshaling event: %w", err)
	}

	// Extract PR number
	if pr, ok := event["pull_request"].(map[string]any); ok {
		if num, ok := pr["number"].(float64); ok {
			ctx.PRNumber = int(num)
		}

		if user, ok := pr["user"].(map[string]any); ok {
			if login, ok := user["login"].(string); ok {
				ctx.Author = login
			}
		}
	}

	// Also check "number" at top level (issue events)
	if ctx.PRNumber == 0 {
		if numStr := os.Getenv("GITHUB_PR_NUMBER"); numStr != "" {
			if n, err := strconv.Atoi(numStr); err == nil {
				ctx.PRNumber = n
			}
		}
	}

	return nil
}
