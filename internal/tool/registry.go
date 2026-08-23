package tool

import (
	"errors"
	"maps"
	"sort"
	"sync"

	adktool "google.golang.org/adk/v2/tool"
)

var ErrToolUnavailable = errors.New("tool is unavailable")

// Registry contains constructed run-scoped tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]adktool.Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]adktool.Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name string, t adktool.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[name] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (adktool.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]

	return t, ok
}

// All returns all registered tools.
func (r *Registry) All() map[string]adktool.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]adktool.Tool, len(r.tools))
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

func (r *Registry) FilteredToolset(names []string) (adktool.Toolset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]adktool.Tool, 0, len(r.tools))
	for _, registered := range r.tools {
		tools = append(tools, registered)
	}

	for _, name := range names {
		if _, exists := r.tools[name]; !exists {
			return nil, ErrToolUnavailable
		}
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })

	return adktool.FilterToolset(NewToolset(tools), adktool.AllowedToolsPredicate(names)), nil
}
