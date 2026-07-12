# duto-ai — Architecture & Implementation Decisions

This document captures all architectural decisions made during the design phase.
It serves as the implementation reference for building duto-ai.

## Package Layout

```
cmd/
  duto-ai/
    main.go                    # CLI entry (cobra or plain flags: run, version)

internal/
  config/                      # Parse config.yaml + workflow YAML
    config.go                  # Global config struct + loader
    workflow.go                # Workflow definition struct + loader
    step.go                    # Step schema types
    resolve.go                 # Env var expansion (${ENV_VAR}), model alias resolution
    validate.go                # Schema validation (required fields, circular deps)

  compiler/                    # YAML steps → ADK v2 workflow.Edge graph
    compiler.go                # Main Compile(workflow, config) → *workflow.Workflow
    dag.go                     # Topological sort, dependency resolution, parallel detection
    node.go                    # Step → AgentNode builder (wires model, tools, prompt)

  prompt/                      # System prompt assembly + Go template rendering
    render.go                  # Go text/template execution ({{ .Steps.X.output }})
    system.go                  # System prompt builder (metadata + event ctx + context files + skills)
    context.go                 # GHA event context extraction (env vars → structured data)

  tool/                        # Tool registry + whitelist resolution
    registry.go                # Global catalog registration (name → tool.Tool)
    resolve.go                 # Glob matching (github.*, github.read-*), merge defaults + step tools
    github/                    # github.* tool implementations
      read.go                  # read-pr, read-diff, list-changed-files, read-comments, etc.
      write.go                 # post-review, post-comment, add-labels, etc.
      client.go                # Shared GitHub HTTP client (GITHUB_TOKEN auth)
    files/                     # files.* implementations
      read.go                  # files.read
    git/                       # git.* implementations (post-MVP)

  provider/                    # Provider factory from config
    factory.go                 # config.provider → model.LLM (dispatches to sapaicore, openai, etc.)

  runtime/                     # Orchestrates the full run: load → compile → execute → output
    run.go                     # Top-level Run(configPath, workflowPath) error
    event.go                   # GHA event context (PR number, repo, etc. from env)
```

### Design Principles

- **`config/` is pure parsing** — no side effects, no network calls. Testable with YAML fixtures.
- **`compiler/` is the heart** — transforms declarative YAML into ADK's imperative graph.
- **`tool/` is a flat registry** — each tool is a `functiontool.New()`. Glob matching over registry keys.
- **`prompt/` handles all text assembly** — system prompt layering and Go template rendering.
- **`runtime/` is the thin orchestrator** — wires everything together, executes the workflow.

---

## DAG Compilation Strategy

### Mapping: YAML → ADK v2 Workflow

| YAML Concept | ADK v2 Concept |
|---|---|
| Step | `AgentNode` wrapping an `llmagent.New()` |
| `needs: [gather]` | `workflow.Edge{From: gatherNode, To: thisNode, Route: nil}` |
| Step with no `needs` | `workflow.Edge{From: workflow.Start, To: thisNode}` |
| `output` field | Written to `session.State["steps.<id>.output"]` |
| Parallel steps (no mutual deps) | Multiple edges from same predecessor → ADK scheduler runs concurrently |

### Compilation Algorithm

1. Parse steps → validate (unique IDs, no cycles via topological sort)
2. For each step: create `llmagent.New()` with resolved model, tools, instruction
3. For each step: create edges from predecessors (or `workflow.Start` if no `needs`)
4. Build `workflow.New(workflowName, edges)`
5. Run via ADK `runner.Run()`

### Output Passing Between Steps

**Decision: Session state (Option A)**

Each step writes its output to `session.State["steps.<id>.output"]`. Downstream steps
access predecessor outputs via Go template rendering: `{{ .Steps.gather.output }}`.

A `FunctionNode` adapter sits between steps to:
1. Read predecessor outputs from session state
2. Render the Go template with those values
3. Feed the rendered prompt to the downstream `AgentNode`

**Why session state over native node input chaining:**
- Multiple predecessors (fan-in) are natural — read N state keys
- Template rendering happens at prompt-assembly time, not graph-wiring time
- Cleaner separation: compiler builds graph shape, prompt renderer handles data flow

---

## Tool System

### Registry Design

```go
type Registry struct {
    tools map[string]tool.Tool  // "github.read-pr" → concrete tool
}

func (r *Registry) Register(name string, t tool.Tool)
func (r *Registry) Resolve(patterns []string) ([]tool.Tool, error)  // glob matching
func (r *Registry) Toolset(patterns []string) tool.Toolset          // wraps as ADK Toolset
```

### Whitelist Merge Logic

Per CONTEXT.md:

```go
func resolveTools(defaults []string, stepTools *[]string) []string {
    switch {
    case stepTools == nil:      return defaults                    // omitted → defaults only
    case len(*stepTools) == 0:  return nil                        // explicit [] → no tools
    default:                    return append(defaults, *stepTools...)  // additive
    }
}
```

### Glob Matching

- `github.*` → all tools where `strings.HasPrefix(name, "github.")`
- `github.read-*` → `path.Match("github.read-*", name)`
- `*` → everything in registry

### Integration with ADK

- Each step gets a `dutoToolset` implementing `tool.Toolset`
- Returns only the resolved tools for that step
- Wired via `llmagent.Config{Toolsets: []tool.Toolset{stepToolset}}`

### MVP Tool Catalog

**Read tools:**
- `github.read-pr` — PR metadata (title, body, author, labels, base/head)
- `github.read-diff` — Full diff patch
- `github.list-changed-files` — List of changed files with status
- `files.read` — Read local file content

**Write tools:**
- `github.post-review` — Post PR review with inline comments
- `github.post-comment` — Post a general PR comment
- `github.add-labels` — Add labels to PR

---

## Provider Integration

### Factory Pattern

```go
func NewLLM(cfg config.Provider, modelName string) (model.LLM, error) {
    switch cfg.Type {
    case "ai-core":
        return sapaicore.New(
            sapaicore.WithURL(cfg.Config["url"]),
            sapaicore.WithResourceGroup(cfg.Config["resource_group"]),
            // ... functional options from config map
        )
    default:
        return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
    }
}
```

### Model Alias Resolution

```go
func resolveModel(stepModel string, aliases map[string]string) string {
    if resolved, ok := aliases[stepModel]; ok {
        return resolved
    }
    return stepModel  // pass as-is
}
```

### Per-Step model_config

- `temperature`, `max_tokens` → `genai.GenerateContentConfig`
- `extra` → provider-specific pass-through (sapaicore's `WithExtraParams`)

### MVP Provider

- AI Core only via `adk-provider-sapaicore` (orchestration mode)
- Interface is ready for future providers (openai, anthropic, openai-compatible)

---

## Testing Strategy

### Three-Tier Approach

| Tier | Build Tag | What It Proves |
|---|---|---|
| **Unit** | (default) | Parsing, compilation, glob resolution, template rendering — deterministic, no I/O |
| **Integration** | `-tags=integration` | Full pipeline with mock LLM — graph execution, output passing, tool dispatch, fail-fast |
| **Smoke** | `-tags=smoke` | Full pipeline with real AI Core LLM + httptest fake GitHub — agentic loop works end-to-end |

### Smoke Test Architecture

```
Real AI Core LLM  +  httptest server (fake GitHub API)  +  faked GHA env vars
         │                        │                              │
         └────────────────────────┼──────────────────────────────┘
                                  ▼
                    duto-ai runtime (full pipeline)
```

**What's faked:**
- `GITHUB_TOKEN` → any string (mock server doesn't validate)
- `GITHUB_EVENT_PATH` → local `testdata/event.json`
- `GITHUB_REPOSITORY`, `GITHUB_EVENT_NAME`, etc. → set in test
- GitHub API → httptest server with canned responses
- GHA environment → env vars set in test setup

**What's real:**
- AI Core LLM — validates reasoning, tool selection, argument formatting
- Full duto-ai pipeline — config → compile → execute → tool calls → output

**Smoke test assertions:**
- ✓ LLM called read-pr, read-diff tools (correct tool selection)
- ✓ Step outputs are non-empty and passed to downstream steps
- ✓ LLM called post-review with valid body (correct write arguments)
- ✓ Review payload has correct PR number, inline comment structure
- ✓ No errors, completed within timeout

### Test Fixtures

```
smoketest/
  testdata/
    config.yaml              # Points provider to real AI Core, GitHub URL to httptest
    pr-review.yaml           # The 3-step workflow
    event.json               # Fake GHA PR event
    fixtures/
      pr.json                # GET /repos/:owner/:repo/pulls/:number
      diff.patch             # GET .../pulls/:number.diff
      files.json             # GET .../pulls/:number/files
      file_content.go        # Raw file for files.read
```

### Golden Files

- `testdata/golden/system-prompt-gather.txt` — expected system prompt for a known config
- Catches regressions in prompt layering without needing an LLM

### mise Tasks

```toml
[tasks.smoke]
run = "go test -tags=smoke ./smoketest/ -v -timeout=5m"

[tasks.integration]
run = "go test -tags=integration ./... -v -timeout=2m"
```

---

## MVP Scope

### IN (first implementation)

| Component | What's Included |
|---|---|
| CLI | `duto-ai run [--config path] workflow.yaml` |
| Config parsing | Full schema, `${ENV_VAR}` expansion, model aliases |
| Workflow parsing | Steps + needs + all Step Schema fields |
| DAG compiler | Full compilation to ADK v2 workflow graph |
| Prompt system | Go template rendering + 5-layer system prompt assembly |
| Tool registry | Registry + glob resolution + whitelist merge |
| Tools (read) | `github.read-pr`, `github.read-diff`, `github.list-changed-files`, `files.read` |
| Tools (write) | `github.post-review`, `github.post-comment`, `github.add-labels` |
| Provider | AI Core via `adk-provider-sapaicore` (orchestration mode) |
| Runtime | Full run loop with fail-fast error propagation |
| Example | `pr-review.yaml` (gather → analyze → report) |
| Tests | Unit + integration + smoke (3-tier) |

### OUT (deferred)

| Deferred | Reason |
|---|---|
| `action.yml` + GoReleaser | Distribution concern — CLI works first |
| `git.*` tools | Not needed for basic PR review |
| `web.*`, `shell.*`, `security.*` tools | Post-MVP categories |
| `files.write`, `files.find`, `files.grep` | Read-only first |
| Multiple providers (openai, anthropic) | AI Core first; interface ready |
| Context files injection | Nice-to-have, system prompt works without |
| Skills resolution | Behavioral .md injection — second PR |
| Retry/fallback | Explicitly excluded per design |
| Structured logging (levels) | Default to info; structured logging later |

### Validation Criteria

```bash
export GITHUB_TOKEN=...
export AI_CORE_AUTH_URL=... AI_CORE_CLIENT_ID=... AI_CORE_CLIENT_SECRET=... AI_CORE_BASE_URL=...

duto-ai run --config .github/ai-workflows/config.yaml .github/ai-workflows/pr-review.yaml
# → reads PR diff, analyzes it, posts inline review comments
```

---

## Code Style

Cherry-picked from `adk-provider-sapaicore`:

- **Copied verbatim:** `.golangci.yaml`, `.gitignore`, `mise.toml`, CI workflow, `CONTRIBUTING.md`
- **Go version:** 1.25
- **Linting:** golangci-lint v2.12.2 with strict config (wsl_v5, errorlint, wrapcheck, funcorder, exhaustive, testpackage)
- **Patterns:** functional options, consumer-defined interfaces, internal/ packages
- **Error wrapping:** lowercase, no "failed" prefix, `%w` for chain preservation
- **Gate:** `mise run check` (build + vet + lint + test) must pass before any commit

---

## Key Dependencies

- `google.golang.org/adk/v2` — Agent runtime (workflow engine, llmagent, tool interfaces)
- `github.com/PedroKlein/adk-provider-sapaicore` — SAP AI Core model provider
- `gopkg.in/yaml.v3` — YAML parsing
- Standard library: `text/template`, `path`, `net/http`, `os`

---

## Credentials & Local Development

duto-ai shares the same SAP AI Core instance as `adk-provider-sapaicore`.
The `.env` file is identical — copy it directly:

```bash
cp ../adk-provider-sapaicore/.env .env
```

Required env vars for smoke tests:
- `AI_CORE_ENDPOINT` — SAP AI Core API endpoint
- `AI_CORE_CLIENT_ID` — OAuth2 client ID
- `AI_CORE_CLIENT_SECRET` — OAuth2 client secret
- `AI_CORE_AUTH_URL` — OAuth2 token endpoint
- `AI_CORE_RESOURCE_GROUP` — Resource group (orchestration mode)

mise loads `.env` automatically via `[env] _.file = ".env"` in `mise.toml`.

See [docs/DEVELOPMENT.md](./DEVELOPMENT.md) for full setup instructions.
