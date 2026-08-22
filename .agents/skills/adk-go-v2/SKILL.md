---
name: adk-go-v2
description: >
  Expert knowledge for Google's ADK Go v2 (`google.golang.org/adk/v2`) — agent construction,
  runner mechanics, tool wiring, session management, workflow graphs, and provider implementation.
  Use when building, debugging, or reviewing code that uses ADK Go v2: agent construction with
  llmagent.New, runner.Run execution loop, functiontool.New typed tools, workflow.New graph
  composition, session.Service implementations, model.LLM provider interfaces, or event streaming
  via iter.Seq2. Triggers on: "adk", "adk-go", "llmagent", "runner.Run", "functiontool",
  "workflow", "session", "model.LLM", "LLMRequest", "GenerateContent", "OutputKey", "ModeChat",
  "ModeSingleTurn", "ModeTask", "AgentNode", "FunctionNode", "JoinNode", "toolset", "Toolset",
  "ProcessRequest", "BeforeModelCallback", "AfterModelCallback", "agent.Context".
  Do NOT use for general Go idioms (use go-dev), concurrency (use go-concurrency),
  or testing mechanics (use go-testing).
---

# ADK Go v2 Reference

Source: ADK Go v2.2.0 (`google.golang.org/adk/v2`). Consult the pinned upstream module source when implementation details must be verified.

## Architecture Stack

```
Runner (drives the loop, manages session persistence)
  └── Agent (llmagent.New — wraps an LLM + tools + instruction)
        ├── Flow (internal: preprocess → callLLM → postprocess → tool exec → loop)
        ├── Tools (functiontool.New — typed Go functions as LLM-callable tools)
        ├── Toolsets (collections, dynamic filtering via Predicate)
        └── SubAgents (ModeChat transfer; ModeTask/ModeSingleTurn native tools)
```

Alternatively, `workflow.New` defines a DAG of Nodes (AgentNode, FunctionNode, JoinNode) with
typed edges, parallel execution, and persistence/resume support.

## Core Rules

### 1. A direct Runner root LlmAgent MUST be ModeChat

When `runner.Config.Agent` is itself an `llmagent`, `Runner.Run` rejects a non-`ModeChat` root. This restriction does not apply to `ModeSingleTurn`/`ModeTask` agents wrapped as workflow nodes or installed as subagent tools:

```go
// BAD — Runner.Run yields an error
agent, _ := llmagent.New(llmagent.Config{Mode: llmagent.ModeSingleTurn, ...})
r, _ := runner.New(runner.Config{Agent: agent, ...})
_ = r // Run reports "root agent must be a chat LlmAgent"

// GOOD
agent, _ := llmagent.New(llmagent.Config{Mode: llmagent.ModeChat, ...})
```

`ModeSingleTurn` and `ModeTask` are valid subagents and workflow nodes subject to ADK placement rules. When nested under a `ModeChat` parent, they are installed automatically as tools.

### 2. The agentic loop is automatic

`Flow.Run` loops `runOneStep` until `IsFinalResponse()`:
- Calls LLM → gets response with function calls → executes tools → feeds results back → repeats
- Stops when: response has no function calls, no function responses, and is not partial

You do NOT implement this loop yourself. If your agent has tools, it will call them iteratively.

### 3. Tools populate LLMRequest via ProcessRequest → PackTool

Every tool calls `toolutils.PackTool(req, tool)` during preprocessing. PackTool does two things:
1. Adds the tool to `req.Tools` map (keyed by name, used for dispatch)
2. Appends `FunctionDeclaration` to `req.Config.Tools[0].FunctionDeclarations`

Provider implementations read tool schemas from `req.Config.Tools` (the `[]*genai.Tool` field).

### 4. ADK v2 uses `ParametersJsonSchema` not `Parameters`

`functiontool.New` generates schemas via `github.com/google/jsonschema-go/jsonschema`:

```go
decl.ParametersJsonSchema = f.inputSchema.Schema()  // *jsonschema.Schema → any
decl.Parameters = nil                                // old *genai.Schema — NOT set
```

Providers MUST handle `ParametersJsonSchema` (type `any`, serializes as standard JSON Schema).
The old `Parameters *genai.Schema` is only set by manually constructed declarations.

### 5. OutputKey stores agent output in session state

```go
llmagent.Config{
    OutputKey: "gather_result",  // final text → session.State["gather_result"]
}
```

After each non-partial event, `maybeSaveOutputToState` concatenates all non-Thought text parts
and writes to `event.Actions.StateDelta[outputKey]`. Other agents/nodes read via session state.

### 6. Event streaming uses iter.Seq2[*session.Event, error]

Everything returns `iter.Seq2[*session.Event, error]` — runner.Run, agent.Run, workflow.Run.
Consume with range-over-func:

```go
for event, err := range r.Run(ctx, userID, sessionID, msg, cfg) {
    if err != nil { /* handle */ }
    if event.LLMResponse.Partial { continue } // skip streaming chunks
    // event.Content.Parts[].Text has the response
}
```

### 7. Session is required — use NewInMemory for simple cases

```go
r, err := runner.NewInMemory("my-app", myAgent)
```

Use `runner.New` when supplying plugins or custom session/artifact/memory services. The runner creates/gets a session per `(userID, sessionID)` pair; `AutoCreateSession: true` skips explicit creation.

### 8. Native subagent modes own ordinary delegation

`llmagent.Config.SubAgents` automatically installs:

- `ModeSingleTurn` children as ordinary agent tools backed by `workflow.RunNode`;
- `ModeTask` children as task tools whose structured result is returned through `finish_task`;
- `ModeChat` children through transfer semantics.

Do not add a custom delegation protocol unless native per-child tools cannot express a tested requirement. `tool/agenttool.New` is different: it starts a separate in-memory runner/session and collapses child events.

`IncludeContents` has only `default` and `none`. `ModeSingleTurn` forces `none`. Branch and isolation scopes filter history; they are not immutable context forks or security sandboxes.

### 9. OutputSchema works with tools in v2.2

`llmagent.Config.OutputSchema` may be used with tools. ADK injects `set_model_response` when a model needs a tool-based structured-output workaround. Task-mode agents use `finish_task` with the declared output schema.

Prefer native `OutputSchema`, workflow node schemas, and `ValidateInput`/`ValidateOutput` over a custom final-text decoder.

### 10. Reuse plugins, TaskRunner, skills, and workflow controls

- `plugin.New` provides run/event/agent/model/tool callbacks; use it for cross-cutting policy/evidence instead of wrappers around every agent.
- `platform.WithTaskRunner` controls standard function-call fan-out; the default runs calls concurrently.
- `workflow.NodeConfig` owns retries, timeout, resume behavior, and per-item parallel workers.
- `workflow.WithMaxConcurrency` bounds statically scheduled graph nodes.
- `workflow.NewRequestInputEvent`/`Resume` provide native HITL pause/resume.
- `tool/skilltoolset` provides skill parsing, sources, preload wrappers, and load tools.
- ADK session, artifact, memory, and telemetry services should be reused rather than mirrored.

## model.LLM Provider Interface

```go
type LLM interface {
    Name() string
    GenerateContent(ctx context.Context, req *LLMRequest, stream bool) iter.Seq2[*LLMResponse, error]
}
```

The `LLMRequest` has:
- `Contents []*genai.Content` — conversation history
- `Config *genai.GenerateContentConfig` — temperature, tools, response format, system instruction
- `Tools map[string]any` — internal dispatch map (DO NOT read this for tool schemas)

Provider reads tool schemas from `req.Config.Tools[].FunctionDeclarations[]`.

## Tool Creation

```go
tool, err := functiontool.New[MyArgs, MyResult](
    functiontool.Config{Name: "search", Description: "Search for information"},
    func(ctx agent.Context, args MyArgs) (MyResult, error) { ... },
)
```

- Input type MUST be a struct or map (pointer OK) — primitives/slices rejected at construction
- Schema is inferred from struct tags via `jsonschema-go` — no manual `*genai.Schema` needed
- The tool implements `ProcessRequest` internally — you never call it yourself

## Thinking Checklist

Before building with ADK, ask:
- **Do I need the Runner?** If you just need one LLM call, you can call `model.GenerateContent`
  directly. Runner adds session persistence, agent transfer, and the tool loop.
- **Do I need a Workflow or just an Agent?** Single agent with tools handles most cases. Workflow
  graphs are for parallel fan-out, typed data flow between nodes, or HITL pause/resume.
- **Should this be a Tool or a SubAgent?** If it is deterministic Go behavior, use `functiontool.New`. If it needs its own model, instruction, tools, or reasoning loop, use a declared `ModeSingleTurn` or `ModeTask` sub-agent.

## Toolset Interface

```go
type Toolset interface {
    Name() string
    Tools(ctx agent.ReadonlyContext) ([]Tool, error)
}
```

Toolsets are dynamic — can return different tools per invocation. Use `tool.FilterToolset`
with `tool.AllowedToolsPredicate` for name-based restriction.

## Workflow Graph

```go
wf, _ := workflow.New("my-workflow", []workflow.Edge{
    {From: workflow.Start, To: gatherNode},
    {From: gatherNode, To: analyzeNode},
    {From: workflow.Start, To: parallelA},
    {From: workflow.Start, To: parallelB},
    {From: parallelA, To: joinNode},
    {From: parallelB, To: joinNode},
    {From: joinNode, To: finalNode},
})
```

Node types:
- `AgentNode` — wraps an agent (defaults to `ModeSingleTurn` in workflow context)
- `FunctionNode` — wraps a typed Go function: `func(ctx, IN) (OUT, error)`
- `JoinNode` — fan-in barrier: waits for all predecessors, receives `map[string]any`
- `DynamicNode` — can spawn sub-nodes at runtime via a scheduler

## Callbacks

```go
llmagent.Config{
    BeforeModelCallbacks: []llmagent.BeforeModelCallback{
        func(ctx agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
            // Return non-nil LLMResponse to skip the actual LLM call (caching)
            // Return (nil, nil) to proceed normally
            // Modify req in-place to alter the request
        },
    },
    BeforeToolCallbacks: []llmagent.BeforeToolCallback{
        func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
            // Return non-nil map to skip tool execution (use as result)
            // Return (nil, nil) to proceed with tool call
            // Modify args in-place to alter tool input
        },
    },
}
```

## NEVER

- **NEVER use a non-ModeChat `llmagent` directly as `runner.Config.Agent`** — `Runner.Run` yields an error; wrap ModeSingleTurn/ModeTask in a workflow node or install them as subagents

- **NEVER read tool schemas from `req.Tools` map in a provider** — that's the internal dispatch
  map (values are `tool.Tool` interfaces); schemas live in `req.Config.Tools[].FunctionDeclarations`

- **NEVER assume `Parameters *genai.Schema` is populated by functiontool.New** — ADK v2 uses
  `ParametersJsonSchema any` exclusively; providers must check both fields with fallback

- **NEVER create an agent without a SessionService** — the runner requires it even for one-shot
  use; use `session.InMemoryService()` for ephemeral workloads

- **NEVER iterate `iter.Seq2` events without checking `event.LLMResponse.Partial`** — partial
  events are streaming chunks; only the final non-partial event is committed to session history

- **NEVER implement a custom final-text structured-output loop solely because an agent has tools** — v2.2 supports `OutputSchema` with tools and task-mode `finish_task`

- **NEVER call `tool/agenttool.New` when same-runner native SubAgents are required** — agenttool starts a separate in-memory runner/session and collapses lineage

- **NEVER describe `IncludeContentsDefault`, branches, or isolation scopes as a context-window fork or security sandbox** — they filter shared session history and infrastructure

- **NEVER call `runner.Run` without consuming all events from the iterator** — the runner persists
  events as they're yielded; stopping iteration mid-stream leaves session state inconsistent

---

**MANDATORY — load `references/internals.md`** when debugging provider integration issues,
understanding the request preprocessing pipeline, or investigating how tool execution flows
through the internal Flow struct.

**Do NOT load `references/internals.md`** for standard agent construction, tool creation, or
workflow graph composition — the SKILL.md body covers those fully.
