# Architecture

This document explains the v0.2.2 implementation. It describes shipped behavior, not the deferred extensibility roadmap.

## System boundary

duto-ai is a process-level runtime. The surrounding pipeline owns triggers, checkout, secrets, token permissions, and runner isolation.

```text
GitHub Actions or local CLI
  -> duto-ai run
     -> load config and workflow YAML
     -> validate and compile an ADK workflow graph
     -> execute one ADK runner
     -> call whitelisted tools
     -> emit structured results and side effects
```

The CLI can run anywhere its binary and provider credentials are available. The packaged composite Action currently supports Linux and macOS runners on AMD64 and ARM64.

## Modules

### `internal/config`

Defines and loads the global configuration and workflow schema. Environment variables are expanded before global configuration parsing. Workflow validation covers empty workflows, duplicate IDs, missing dependencies, cycles, timeouts, and iteration limits.

Model aliases map user-facing names to provider model names.

Known limitation: unknown YAML fields, unknown tools, and unmatched tool globs are not strict validation errors in v0.2.2.

### `internal/compiler`

Transforms validated steps into ADK nodes and edges.

| Workflow field | ADK behavior |
|---|---|
| step | `AgentNode` wrapping a `llmagent` |
| `needs` | directed edge from predecessor to successor |
| multiple `needs` | ADK fan-in handling |
| no `needs` | edge from workflow start |
| `model` | model resolver lookup |
| `tools` | per-step ADK toolset |
| `max_iterations` | before-model iteration limiter |
| `timeout` | node timeout |
| `retry` | node retry configuration |
| `output` present | ADK `OutputKey` at `steps.<id>.output` |

A step prompt becomes part of the agent instruction. `.md` and `.txt` values are loaded as files. Event and environment templates render before execution.

ADK passes predecessor output to successor nodes. v0.2.2 does not implement public named or typed outputs. The text supplied in `output:` only enables storage.

### `internal/prompt`

Builds each step instruction from these layers:

1. Step metadata and available tool names
2. Pipeline event context
3. Configured context files
4. Resolved skill files
5. Step `system` text and task prompt

Skills are discovered under `.github/ai-workflows/skills/` and may also be referenced by path.

### `internal/provider`

Constructs the bundled provider and resolves a `model.LLM` for each configured model name. Runtime model instances are cached by resolved model name for the duration of one workflow run.

The provider seam is internal in v0.2.2. Adding another provider requires code changes. `model_config.temperature` and `max_tokens` reach ADK generation config; `model_config.extra` is parsed but not forwarded.

### `internal/tool`

The registry maps a dot-namespaced name to an ADK tool. Glob resolution uses `path.Match`. Resolved tools are sorted for deterministic model declarations.

Tool list behavior:

```text
step tools omitted    -> configured defaults
step tools []         -> no tools
step tools non-empty  -> configured defaults plus step tools
```

Registered namespaces:

- `github.*`: 15 read/write GitHub operations
- `files.*`: read, find, grep
- `git.*`: log, blame, show, diff
- `shell.*`: command execution
- `web.*`: fetch and request

The exact catalog is maintained in `README.md` and the package `RegisterAll` functions.

### `internal/runtime`

Coordinates loading, provider construction, registry assembly, compilation, ADK runner setup, event collection, and result formatting.

The runtime uses an in-memory ADK session service. State lasts for one invocation. Errors fail the workflow and mark pending steps as skipped. Completed-step output remains available in the returned partial result.

## Results and GitHub integration

`RunWithResult` returns workflow and step statuses, outputs, durations, and failure details. The CLI formats the result as text, JSON, or Markdown.

Inside GitHub Actions it also writes:

- `status`, `workflow`, `duration_ms`, and `failed_step` to `GITHUB_OUTPUT`
- the Markdown report to `GITHUB_STEP_SUMMARY`

The composite Action maps these values to its public outputs.

## Security model

Tool visibility is a capability boundary for the model. It is not process isolation.

- File operations reject paths outside the configured root.
- Git read operations execute with the repository root as cwd.
- Shell commands can access anything the runner account can access.
- Web tools can reach anything available through the runner network.
- GitHub writes are limited by the token's repository permissions.

Pipeline authors must treat workflow files as code, use least-privilege tokens, and avoid exposing shell, network, or write tools to untrusted pull-request content. See `SECURITY.md`.

## Testing

| Tier | Command | Boundary |
|---|---|---|
| Unit | `mise run test` | parsing, graph compilation, tools, formatting |
| Integration | `mise run integration` | full ADK graph with a mock model |
| Smoke | `mise run smoke` | live model with a fake GitHub API |
| Live scenarios | duto-test Actions workflow | released binary in a real pipeline |

`mise run check` runs build, vet, lint, and race tests. Live acceptance runs after deterministic checks.

## Deferred design work

The `duto-ai-extensibility` plan owns the next architecture decisions:

- provider registration and additional adapters
- custom tool registration and capability metadata
- strict config and tool validation
- named and typed outputs
- repository mutation, search, and dependency-security tools
- execution-context trust policy

Those designs are gated by ranked use cases. This document should not describe them as shipped until their acceptance criteria pass.
