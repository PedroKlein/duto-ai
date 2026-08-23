package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adktool "google.golang.org/adk/v2/tool"
)

var (
	ErrInvalidGuard     = errors.New("invalid tool guard")
	ErrToolNotAllowed   = errors.New("tool is not allowed")
	ErrToolCallLimit    = errors.New("tool call limit exceeded")
	ErrToolRequestLimit = errors.New("tool request byte limit exceeded")
	ErrToolResultLimit  = errors.New("tool result byte limit exceeded")
)

type ToolLimit struct {
	MaxCalls        int           `json:"max_calls"`
	Timeout         time.Duration `json:"-"`
	MaxRequestBytes int           `json:"max_request_bytes"`
	MaxResultBytes  int           `json:"max_result_bytes"`
}

type RuntimeLimit struct {
	MaxCalls int
	Timeout  time.Duration
}

type ScopeLimit struct {
	Names    []string
	MaxCalls int
	Timeout  time.Duration
	Tools    map[string]ToolLimit
}

type guardCounters struct {
	mu         sync.Mutex
	runCalls   int
	scopeCalls map[string]int
	toolCalls  map[string]int
}

type Guard struct {
	scope       string
	allowed     map[string]struct{}
	runLimit    RuntimeLimit
	scopeLimit  ScopeLimit
	runDeadline time.Time
	deadline    time.Time
	counters    *guardCounters
}

func NewGuards(run RuntimeLimit, scopes map[string]ScopeLimit) (map[string]*Guard, error) {
	if run.MaxCalls < 0 || run.Timeout <= 0 {
		return nil, ErrInvalidGuard
	}

	started := time.Now()
	counters := &guardCounters{scopeCalls: make(map[string]int, len(scopes)), toolCalls: make(map[string]int)}
	guards := make(map[string]*Guard, len(scopes))

	for scopeName, scope := range scopes {
		guard, err := newGuard(run, scopeName, scope, started, counters)
		if err != nil {
			return nil, err
		}

		guards[scopeName] = guard
	}

	return guards, nil
}

func newGuard(run RuntimeLimit, scopeName string, scope ScopeLimit, started time.Time, counters *guardCounters) (*Guard, error) {
	if scopeName == "" || scope.MaxCalls < 0 || scope.Timeout <= 0 {
		return nil, ErrInvalidGuard
	}

	allowed := make(map[string]struct{}, len(scope.Names))
	for _, name := range scope.Names {
		limit, exists := scope.Tools[name]
		if !exists || limit.MaxCalls <= 0 || limit.Timeout <= 0 || limit.MaxRequestBytes < 0 || limit.MaxResultBytes < 0 {
			return nil, ErrInvalidGuard
		}

		allowed[name] = struct{}{}
	}

	if len(allowed) > 0 && (scope.MaxCalls == 0 || run.MaxCalls == 0) {
		return nil, ErrInvalidGuard
	}

	toolLimits := make(map[string]ToolLimit, len(scope.Tools))
	for name, limit := range scope.Tools {
		if _, exists := allowed[name]; !exists {
			return nil, ErrInvalidGuard
		}

		toolLimits[name] = limit
	}

	scope.Tools = toolLimits
	scope.Names = slices.Clone(scope.Names)

	return &Guard{
		scope:       scopeName,
		allowed:     allowed,
		runLimit:    run,
		scopeLimit:  scope,
		runDeadline: started.Add(run.Timeout),
		deadline:    started.Add(scope.Timeout),
		counters:    counters,
	}, nil
}

func (g *Guard) Allows(name string) bool {
	if g == nil {
		return false
	}

	_, allowed := g.allowed[name]

	return allowed
}

func (g *Guard) Before(name string) error {
	if g == nil {
		return ErrInvalidGuard
	}

	if _, allowed := g.allowed[name]; !allowed {
		return fmt.Errorf("%w: %s", ErrToolNotAllowed, name)
	}

	g.counters.mu.Lock()
	defer g.counters.mu.Unlock()

	g.counters.runCalls++
	g.counters.scopeCalls[g.scope]++
	key := g.scope + "\x00" + name
	g.counters.toolCalls[key]++

	if g.counters.runCalls > g.runLimit.MaxCalls || g.counters.scopeCalls[g.scope] > g.scopeLimit.MaxCalls || g.counters.toolCalls[key] > g.scopeLimit.Tools[name].MaxCalls {
		return fmt.Errorf("%w: %s", ErrToolCallLimit, name)
	}

	return nil
}

func (g *Guard) Context(parent context.Context, name string) (context.Context, context.CancelFunc, error) {
	if g == nil {
		return nil, nil, ErrInvalidGuard
	}

	limit, allowed := g.scopeLimit.Tools[name]
	if !allowed {
		return nil, nil, fmt.Errorf("%w: %s", ErrToolNotAllowed, name)
	}

	deadline := minTime(g.runDeadline, g.deadline, time.Now().Add(limit.Timeout))
	ctx, cancel := context.WithDeadline(parent, deadline)

	return ctx, cancel, nil
}

func (g *Guard) Limit(name string) (ToolLimit, bool) {
	if g == nil {
		return ToolLimit{}, false
	}

	limit, exists := g.scopeLimit.Tools[name]

	return limit, exists
}

func (g *Guard) BeforeToolCallback() llmagent.BeforeToolCallback {
	return func(ctx agent.Context, current adktool.Tool, args map[string]any) (map[string]any, error) {
		if !g.Allows(current.Name()) {
			return nil, nil //nolint:nilnil // non-catalog native tools keep their own policy.
		}

		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("tool context: %w", err)
		}

		if err := g.Before(current.Name()); err != nil {
			return nil, err
		}

		if err := checkBytes(args, g.scopeLimit.Tools[current.Name()].MaxRequestBytes, ErrToolRequestLimit); err != nil {
			return nil, err
		}

		return nil, nil //nolint:nilnil // nil continues native ADK tool execution
	}
}

func (g *Guard) AfterToolCallback() llmagent.AfterToolCallback {
	return func(_ agent.Context, current adktool.Tool, _, result map[string]any, _ error) (map[string]any, error) {
		if !g.Allows(current.Name()) {
			return nil, nil //nolint:nilnil // non-catalog native tools keep their own policy.
		}

		if err := checkBytes(result, g.scopeLimit.Tools[current.Name()].MaxResultBytes, ErrToolResultLimit); err != nil {
			return nil, err
		}

		return nil, nil //nolint:nilnil // preserve the native tool result
	}
}

func checkBytes(value map[string]any, limit int, limitErr error) error {
	if limit == 0 {
		return nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding tool payload: %w", err)
	}

	if len(encoded) > limit {
		return limitErr
	}

	return nil
}

func minTime(values ...time.Time) time.Time {
	minimum := values[0]
	for _, value := range values[1:] {
		if value.Before(minimum) {
			minimum = value
		}
	}

	return minimum
}
