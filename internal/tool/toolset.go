package tool

// Toolset wraps a set of resolved tools for use with ADK.
type Toolset struct {
	name  string
	tools []Tool
}

// NewToolset creates a Toolset from resolved tools.
func NewToolset(tools []Tool) *Toolset {
	return &Toolset{
		name:  "duto",
		tools: tools,
	}
}

// Name returns the toolset name.
func (ts *Toolset) Name() string {
	return ts.name
}

// Tools returns the resolved tools.
func (ts *Toolset) Tools() []Tool {
	return ts.tools
}
