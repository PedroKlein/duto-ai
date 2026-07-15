package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// EventContext holds GitHub Actions event data extracted from environment.
type EventContext struct {
	Repo        string
	PRNumber    int
	IssueNumber int
	Author      string
	EventName   string
	EventPath   string
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

	// Extract PR number and author from pull_request events.
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

	// Extract issue number and author from issues events.
	ctx.parseIssueEvent(event)

	// Fallback: check env var override for PR number.
	if ctx.PRNumber == 0 {
		if numStr := os.Getenv("GITHUB_PR_NUMBER"); numStr != "" {
			if n, err := strconv.Atoi(numStr); err == nil {
				ctx.PRNumber = n
			}
		}
	}

	return nil
}

func (ctx *EventContext) parseIssueEvent(event map[string]any) {
	issue, ok := event["issue"].(map[string]any)
	if !ok {
		return
	}

	if num, numOK := issue["number"].(float64); numOK {
		ctx.IssueNumber = int(num)
	}

	if ctx.Author != "" {
		return
	}

	user, userOK := issue["user"].(map[string]any)
	if !userOK {
		return
	}

	if login, loginOK := user["login"].(string); loginOK {
		ctx.Author = login
	}
}
