# ADK Go v2 Internals

Source: `/Users/i572543/Dev/pi-repos/repos/github.com/google/adk-go/v2.0.0/internal/`

## Request Preprocessing Pipeline

The `Flow` struct in `internal/llminternal/base_flow.go` runs these processors in order
on every LLM call:

```
1. basicRequestProcessor    — clones GenerateContentConfig onto req.Config
2. toolProcessor            — resolves tools from toolsets (calls Toolset.Tools())
3. authPreprocessor         — handles auth-related tool preprocessing
4. RequestConfirmation      — handles HITL confirmation state
5. instructionsRequestProcessor — builds system instruction from Instruction/GlobalInstruction
6. identityRequestProcessor — adds agent identity/transfer instructions
7. ContentsRequestProcessor — builds req.Contents from session history
8. nlPlanningRequestProcessor — natural language planning markers
9. codeExecutionRequestProcessor — optimizes data files for code execution
10. outputSchemaRequestProcessor — injects output schema (tool-based workaround if tools present)
11. AgentTransferRequestProcessor — adds transfer_to_agent tool declarations
12. removeDisplayNameIfExists — cleans display_name from content parts
```

After these processors run, `toolPreprocess` iterates each tool calling `ProcessRequest`
(which calls `PackTool` to add declarations to `req.Config.Tools`), then `toolsetPreprocess`
does the same for toolsets that implement `RequestProcessor`.

## Flow.Run Loop Mechanics

```go
func (f *Flow) Run(ctx) iter.Seq2[*Event, error] {
    for {
        lastEvent := f.runOneStep(ctx)  // preprocess → callLLM → postprocess → handle tools
        if lastEvent.IsFinalResponse() && !isThoughtOnlyTurn(lastEvent) {
            return  // DONE — agent produced a final text response
        }
        // Otherwise loop: tool results were appended to session, next call includes them
    }
}
```

`IsFinalResponse()` is true when:
- No function calls in the response
- No function responses
- Not partial (not a streaming chunk)
- Not a trailing code execution result
- OR `SkipSummarization` is set (HITL interrupts)

## runOneStep Internals

1. Create empty `LLMRequest{Model: f.Model.Name()}`
2. Run all request preprocessors (populates Config, Contents, Tools)
3. Call `f.callLLM(ctx, req)` → streams `LLMResponse`
4. Run response postprocessors
5. If response has function calls → `handleFunctionCalls`:
   - Looks up tool by name in `req.Tools` map
   - Runs `BeforeToolCallbacks` (can skip/replace)
   - Calls `tool.Run(ctx, args)` → gets result map
   - Runs `AfterToolCallbacks` (can replace result)
   - Builds `FunctionResponse` content parts
   - Creates event with all function responses
6. If agent transfer → delegate to sub-agent's `RunNode`

## Tool Dispatch (handleFunctionCalls)

```go
func (f *Flow) handleFunctionCalls(ctx, tools map[string]tool.Tool, resp, ...) (*Event, error) {
    for _, fc := range functionCalls(resp.Content) {
        tool := tools[fc.Name]  // lookup from req.Tools map
        // Run before-tool callbacks
        result, err := tool.Run(ctx, fc.Args)
        // Run after-tool callbacks
        // Build FunctionResponse part
    }
    // Return event with all FunctionResponse parts
}
```

Tools are cast to the internal `runnableTool` interface:
```go
type runnableTool interface {
    Tool
    Declaration() *genai.FunctionDeclaration
    Run(ctx agent.Context, args any) (map[string]any, error)
}
```

## Runner Node Path vs Agent Path

The runner has TWO execution paths:

**Node path** (used when root is LlmAgent — the common case):
- Wraps agent in a synthetic single-node workflow: `START → agentNode`
- Runs through the workflow engine (scheduler, persistence, resume)
- Supports HITL pause/resume via workflow state
- All events get persisted to session

**Agent path** (used when root is a custom agent.New):
- Directly calls `agent.Run(ctx)`
- Simpler: no workflow wrapper, no scheduler
- Still persists events and runs plugin callbacks

## Session State Access Patterns

From a tool/callback:
```go
func(ctx agent.Context, args MyArgs) (MyResult, error) {
    // Read state
    val, err := ctx.State().Get("some_key")
    
    // Write state (via event actions — persisted on next event)
    ctx.Actions().StateDelta["my_output"] = "result"
    
    // Read session history
    events := ctx.Session().Events()
    for i := 0; i < events.Len(); i++ {
        ev := events.At(i)
        // ...
    }
}
```

## Instruction Templating

Instructions support `{key_name}` placeholders resolved from session state:

```go
llmagent.Config{
    Instruction: "You are helping with PR #{pr_number} in {repo_name}.",
}
// At runtime: state.Get("pr_number"), state.Get("repo_name")
```

- `{key?}` — optional (no error if missing)
- `{artifact.key_name}` — inserts artifact text content
- `InstructionProvider` — dynamic function, called each invocation, no auto-substitution

## Sub-Agent Mode Mechanics

When `llmagent.New` is called with sub-agents, it auto-installs tools based on mode:

| Sub-agent Mode | Tool Installed | Behavior |
|---|---|---|
| ModeChat | `transfer_to_agent_{name}` | Transfers conversation control |
| ModeSingleTurn | `{name}` (as tool) | Called like a function, returns output |
| ModeTask | `{name}` (task tool) | Multi-turn task with finish signal |

ModeSingleTurn sub-agents are the most useful for orchestration — they're invisible tools
that delegate to a full agent.

## Event Structure

```go
type Event struct {
    model.LLMResponse           // embedded: Content, UsageMetadata, etc.
    ID             string
    Timestamp      time.Time
    InvocationID   string
    Author         string       // agent name or "user"
    Branch         string       // isolation for parallel branches
    IsolationScope string       // task isolation
    Actions        EventActions // state delta, transfer, skip flags
    Output         any          // workflow node output (distinct from LLM content)
    NodeInfo       *NodeInfo    // workflow engine metadata
    LongRunningToolIDs []string // HITL: paused tool call IDs
}
```

`event.Content.Parts` is where the actual LLM response lives. Each Part can have:
- `Text` — model text output
- `FunctionCall` — model requesting a tool call
- `FunctionResponse` — result of a tool call
- `Thought` — thinking/reasoning (from extended thinking models)

## StrictContextMock for Testing

```go
type fakeContext struct {
    agent.StrictContextMock  // panics on un-overridden methods
}

func TestMyTool(t *testing.T) {
    ctx := &fakeContext{agent.StrictContextMock{Ctx: t.Context()}}
    // Override methods as needed on fakeContext
    result, err := myTool.Run(ctx, args)
}
```

Prefer `StrictContextMock` over manual interface implementation — it survives interface growth.

## Testing Without Runner (Direct LLM Mock)

To test agent behavior without a real LLM, implement `model.LLM`:

```go
type mockLLM struct {
    responses []*model.LLMResponse
    callIdx   int
}

func (m *mockLLM) Name() string { return "mock" }

func (m *mockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
    return func(yield func(*model.LLMResponse, error) bool) {
        resp := m.responses[m.callIdx]
        m.callIdx++
        yield(resp, nil)
    }
}
```

Then wire it: `llmagent.Config{Model: &mockLLM{...}, ...}`

Key insight: if your mock returns a `FunctionCall` part, the runner will execute the tool and
call `GenerateContent` again with the `FunctionResponse` in history. Plan your mock responses
accordingly:
1. First response: `FunctionCall{Name: "my-tool", Args: {...}}`
2. Second response: `Text` part with final answer (tool result is in `req.Contents`)

## Testing Tool Execution in Isolation

Tools can be tested without any ADK machinery:

```go
func TestSearchTool(t *testing.T) {
    tool, _ := functiontool.New[SearchArgs, SearchResult](cfg, handler)
    
    // Cast to runnableTool interface (internal but stable)
    type runner interface {
        Run(agent.Context, any) (map[string]any, error)
    }
    r := tool.(runner)
    
    ctx := &fakeContext{agent.StrictContextMock{Ctx: t.Context()}}
    result, err := r.Run(ctx, map[string]any{"query": "test"})
    // assert on result
}
```

Note: args are always `map[string]any` — ADK handles struct conversion internally.
