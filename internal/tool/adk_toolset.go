package tool

import (
	"google.golang.org/adk/v2/agent"
	adktool "google.golang.org/adk/v2/tool"
)

type dutoToolset struct {
	tools []adktool.Tool
}

// NewToolset creates an ADK-compatible Toolset from resolved tools.
func NewToolset(tools []adktool.Tool) adktool.Toolset {
	return &dutoToolset{tools: tools}
}

func (ts *dutoToolset) Name() string {
	return "duto"
}

func (ts *dutoToolset) Tools(_ agent.ReadonlyContext) ([]adktool.Tool, error) {
	return ts.tools, nil
}
