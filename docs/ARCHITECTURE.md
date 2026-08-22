# Architecture

This document explains the v0.2.2 implementation. It describes shipped behavior, not the deferred extensibility roadmap.

## Product center

Duto is a host-neutral CLI and runtime for bounded AI workflow DAGs. A human, coding agent, planner, or CI job may produce a workflow; duto validates the finite graph, compiles it to ADK Go, executes it, and returns structured results.

Local CLI execution is the primary path. GitHub Actions is an official host adapter over the same runtime. Host adapters own triggers, checkout, credentials, event mapping, and result publication; portable workflow YAML does not depend on GitHub.

Open-ended planning remains outside the runtime. A planner may emit a complete duto workflow, but duto does not initially maintain a backlog, rewrite its own graph, or create arbitrary runtime agents. See [ADR 008](adr/008-product-center-and-delivery-layers.md).

## System boundary

duto-ai is a process-level runtime. The surrounding caller owns triggers, checkout, secrets, token permissions, and runner isolation.

```text
local user, agent, planner, or host adapter
  -> duto-ai validate / plan / run
     -> load trusted config and portable workflow YAML
     -> validate and compile one immutable effective plan
     -> validate: emit diagnostics only
     -> plan: emit the complete redacted plan only
     -> run: execute one ADK runner with fresh in-memory services
     -> emit one typed result and redacted evidence
```

M1 does not publish side effects. The M2 Action adapter maps host inputs and projects the same one-shot result. M3 adds staged safe outputs and trusted publication.

The CLI can run anywhere its binary and provider credentials are available. The packaged composite Action currently supports Linux and macOS runners on AMD64 and ARM64.

## Modules

The module descriptions below distinguish shipped v0.2.2 behavior from the accepted CLI-first target. Accepted target semantics are canonical in ADRs 001–008 and must not be presented as shipped until implemented.

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

In shipped v0.2.2, `RunWithResult` returns workflow and step statuses, outputs, durations, and failure details. The CLI formats the result as text, JSON, or Markdown.

In the accepted M1 contract, `validate`, `plan`, and `run` each emit one text or schema-versioned JSON payload; logs stay on stderr. One-shot evidence is not a resumable checkpoint/session store.

Inside the current GitHub Action, v0.2.2 also writes:

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

## Delivery direction

Future work is layered so optional host features do not become core prerequisites:

1. **CLI-first core:** strict YAML, effective-plan inspection, typed static DAG execution, exact model/tool/prompt/result contracts, limits, and local evidence.
2. **One-shot GitHub adapter:** trusted event mapping, least-privilege permissions, summaries, outputs, and artifact upload over the same core runtime.
3. **Bounded authoring and safe outputs:** admitted workspace/process/Git mutation and a fixed publisher.
4. **Future durable hosts:** persistent pause/resume, encrypted GitHub state, cross-runner recovery, lifecycle races, and asynchronous reply correlation.

Strict validation, typed outputs, trust derivation, finite native subagents, and admitted bounded file/read-only Git/GitHub read-review/web/`shell.run` compatibility are accepted M1 work. One-shot Action mapping is M2. Workspace/Git mutation, staged safe outputs, and trusted publication are M3. Public plugin/custom-provider extensibility and additional speculative adapters remain unscheduled. The durable-session decisions already made are preserved by [ADR 008](adr/008-product-center-and-delivery-layers.md) but are not M1 construction dependencies.

This document should not describe deferred designs as shipped until their acceptance criteria pass.
