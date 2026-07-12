package tool

import (
	"google.golang.org/adk/v2/agent"
	adktool "google.golang.org/adk/v2/tool"
)

// ADKToolset implements adktool.Toolset by wrapping resolved duto tools.
type ADKToolset interface {
	adktool.Toolset
}

type dutoToolset struct {
	name  string
	tools []Tool
}

// NewADKToolset creates an ADK-compatible Toolset from resolved tools.
func NewADKToolset(tools []Tool) ADKToolset {
	return &dutoToolset{
		name:  "duto",
		tools: tools,
	}
}

func (ts *dutoToolset) Name() string {
	return ts.name
}

func (ts *dutoToolset) Tools(_ agent.ReadonlyContext) ([]adktool.Tool, error) {
	result := make([]adktool.Tool, 0, len(ts.tools))

	for _, t := range ts.tools {
		if adkTool, ok := t.(adktool.Tool); ok {
			result = append(result, adkTool)
		}
	}

	return result, nil
}
