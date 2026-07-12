package tool

import (
	"maps"
	"sort"
	"sync"
)

// Tool is a minimal interface for a tool in the registry.
// It wraps ADK's tool interface with a name getter.
type Tool interface {
	Name() string
}

// Registry is a catalog of all available tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name string, t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[name] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]

	return t, ok
}

// All returns all registered tools.
func (r *Registry) All() map[string]Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]Tool, len(r.tools))
	maps.Copy(result, r.tools)

	return result
}

// Names returns all registered tool names sorted alphabetically.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for k := range r.tools {
		names = append(names, k)
	}

	sort.Strings(names)

	return names
}
