# ADR 002: Tool catalog and policy

- **Status:** Accepted; M1 implementation shipped
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0
- **Scope:** Built-in tool identity, exact allowlists, resource bounds, runtime guards, and ADK wiring

## M1 implementation amendment

M1 ships the fixed read/process catalog, strict selectors, flat profiles, trusted ceilings, parent subset checks, exact per-tool limits, run-scoped construction, attempt accounting, and family boundary checks. Host trust-context derivation, mutation/effect classes, and publication remain M3 work under ADR 003.

## Context

The superseded resolver added step tools to defaults and treated malformed or unmatched glob patterns leniently. A reviewer could not determine a step's authority from the step declaration alone.

Duto needs a small policy compiler, not a second tool runtime. ADK already supplies `tool.Tool`, `tool.Toolset`, `tool.FilterToolset`, typed `functiontool.New`, tool callbacks, confirmations, and caller-controlled function-call scheduling.

## Decision drivers

- Registration makes a tool available; it never authorizes it.
- Omission must be safe and obvious.
- A named agent or step cannot widen its parent/orchestrator's direct tools or resources; delegation remains inside a finite, statically checked authority envelope.
- Workspace, command, repository, domain, call, byte, and time bounds remain inspectable.
- Tool execution stays native ADK behavior.
- New policy fields arrive with the tool family that uses them, not years earlier.

## Alternatives

### A. Additive defaults

Root defaults and step lists are merged. This is convenient but hides authority and preserves the current bug.

### B. Exact scopes with flat composable profiles

Each scope has an exact set compiled from flat selector-only profiles and explicit add/remove operations. Children can inherit explicitly or select an exact subset; limits remain separate.

### C. Capability-only policy

Workflows select abstract capabilities and the runtime expands them to tools. This adds a second public vocabulary and makes model-visible tools harder to predict.

| Concern | A: additive | B: exact sets | C: capabilities |
|---|---|---|---|
| Local review | Poor | Strong | Indirect |
| Safe omission | Ambiguous | No tools | Depends on expansion |
| Migration | Easy but unsafe | Mechanical | Broad rewrite |
| Implementation | Small | Small | Medium |

## Decision

Select **B: exact scopes with flat composable profiles**.

### Source semantics

Trusted configuration may define centrally reusable profiles; a portable workflow may define workflow-local profiles. Both are flat selector lists:

```yaml
# trusted duto.yaml
tool_profiles:
  source-review: [files.read, files.grep, git.read.diff]

# portable workflow
tool_profiles:
  history: [git.read.log, git.read.show]
```

Profile names must be unique across both documents. Profiles cannot reference profiles, own limits, contain host bindings, or discover plugins. Trusted profiles are reusable spelling, not grants: a portable scope must explicitly add one, and the trusted catalog ceiling still bounds every expansion.

Every workflow, named-agent, and step scope accepts either an exact selector array or a composable expression:

```yaml
tools: [files.read, git.read.diff]
```

```yaml
tools:
  from: empty
  add_profiles: [source-review]
  add: [git.read.log]
  remove_profiles: [history]
  remove: [files.grep]
```

The array is shorthand for `from: empty` plus `add`. The object is closed to the five fields shown. `from` is `empty` or `parent` and defaults to `empty`.

- Omitted `tools`, `tools: []`, `tools: {}`, and all-empty additions mean no direct tools.
- Inheritance is never implicit; it requires `from: parent`.
- `from: parent` is invalid at workflow root.
- A workflow selects only from the trusted admitted ceiling.
- A top-level named agent's parent is workflow authority.
- A step's parent is its named agent when present, otherwise workflow authority.
- A delegated named agent's parent is its declaring orchestrator's direct authority. Each use must compile as a subset if an agent has multiple parents.
- Every child effective set is a subset of its parent. Calling a child never makes the child's tools directly callable by the parent.

Limits are separate from profiles and tool expressions. Per-tool limits use exact tool names under `tool_limits`; there are no per-profile call counts.

## Built-in catalog

The internal catalog records only facts needed before construction:

```go
type Definition struct {
    Name         string
    Capabilities []Capability
    SideEffect   SideEffect
}
```

Required metadata:

- exact model-visible name;
- capability classes used by trust policy;
- side-effect class;
- family-owned policy validator/builder.

Current capability classes are:

- `workspace.read`
- `workspace.mutate`
- `git.read`
- `git.mutate`
- `git.publish`
- `process.exec`
- `network.read`
- `network.mutate`
- `github.read`
- `github.mutate`
- `agent.call`

The classes are policy facts, not workflow selectors.

M1 registers only the accepted hierarchical names for the shipped families. Removed names are not retained as aliases.

| Family | M1 names | Later |
|---|---|---|
| Files | `files.read`, `files.find`, `files.grep` | Mutation in M3 |
| Git | `git.read.log`, `git.read.blame`, `git.read.show`, `git.read.diff` | Local authoring and publication in M3 |
| GitHub | `github.read.issue`, `github.read.pr`, `github.read.diff`, `github.read.changed-files`, `github.read.comments`, `github.read.reviews`, `github.read.checks`, `github.read.search-issues` | Action mapping in M2; mutation/publication in M3 |
| Web | Bounded `web.fetch` | Additional search capabilities remain unscheduled |
| Shell/process | Opt-in `shell.run` under trusted command, workspace, environment, time, output, and call ceilings | M3 may add fixed aliases that narrow or replace compatibility |
| Agent | One native tool per admitted declared child | Persistent delegated conversations remain future-host work |

M1 GitHub tools only read runtime-bound repository and review data. Posting reviews or comments, changing labels, and other mutation or publication are absent. A later write taxonomy may use names such as `github.write.comment`, but that example does not admit or finalize an M3 tool.

A catalog version changes only when model-visible names or policy semantics change. A digest is useful in the effective plan, but a public per-layer decision-trace type is not required in M1.

## Selectors and deterministic expansion

Portable lists accept exact names such as `files.read`, `git.read.diff`, and `github.read.pr`. A wildcard may appear only as the final complete segment of an allowed leaf namespace. M1 allows `files.*`, `git.read.*`, and `github.read.*`; `web.fetch` and `shell.run` are selected exactly. The broader `git.*` and `github.*` selectors are invalid, as is portable global `*`. Names are case-sensitive ASCII. A centrally maintained explicit profile is the reviewable alternative.

The compiler evaluates a tool expression in this fixed order:

1. Initialize from empty or the exact parent set.
2. Expand `add_profiles` in source order and each profile in declaration order.
3. Expand `add` in source order; wildcard matches use catalog-name byte order.
4. Expand `remove_profiles` in source order and each profile in declaration order.
5. Expand `remove` in source order; wildcard matches use catalog-name byte order.
6. Emit final names in catalog-name byte order.

Insertion is idempotent. Overlapping profiles, exact names, and wildcards deduplicate by exact model-visible tool name and create one authority/counter domain. Removal is evaluated against the accumulated set immediately before each selector; every removal selector must remove at least one currently present tool. Authority is a set, not profile-provenance reference counting.

Validation is fail-closed with stable diagnostics:

- `duplicate_tool_profile` for a trusted/workflow profile collision;
- `unknown_tool_profile` for an unknown profile;
- `unknown_tool` for an unknown exact name;
- `invalid_tool_selector` for malformed syntax;
- `unmatched_tool_selector` for a valid wildcard with no catalog match;
- `tool_ceiling_exceeded` for selection outside trusted authority;
- `tool_authority_widening` for selection outside parent authority;
- `invalid_tool_removal` when a removal matches no currently accumulated tool;
- `invalid_tool_parent` for `from: parent` at workflow root.

These are validation/admission failures. No model or tool call occurs and no partial effective plan is emitted.

## Resource and limit policy

Tool-family policy is closed and typed. Concrete roots, credentials, tokens, fixed executable paths, command argv, and absolute ceilings belong only to trusted configuration. Portable YAML may request exact narrower per-tool limits:

```yaml
tool_limits:
  files.read:
    max_calls: 12
    timeout: 15s
    max_result_bytes: 262144
```

Keys are exact effective tool names, never profiles or wildcards. An entry for a tool outside that scope's effective set is invalid. Children may only narrow. Family records continue to own symbolic paths, refs, domains, methods, repositories, commands, and other resource categories.

Limit ownership is deterministic:

- trusted runtime configuration owns hard maxima for every dimension and all concrete resources;
- workflow `limits` owns requested run-wide calls, iterations, timeout, artifact bytes, graph concurrency, and parallel-call limits;
- named-agent `limits` owns narrower activation budgets;
- step `limits` owns narrower step budgets;
- exact `tool_limits` owns narrower per-tool calls, timeout, request/result bytes, and family resources.

Narrowing is structural: write may narrow to read; numeric/time maxima may decrease; categorical sets may shrink. Omission inherits an already-bounded parent value and never means unlimited. Unprovable or explicit widening rejects rather than silently clamping.

Each attempted call atomically debits per-tool, step/agent, and run counters before execution. Retry attempts consume counters; timeout, cancellation, and failure do not refund them. The effective deadline is the earliest caller, run, activation, step, per-tool, and family deadline. A common before-tool guard performs admission/accounting, and handlers repeat resource and byte checks at the I/O seam.

## Effective plan

The pure M1 compiler intersects trusted ceilings, profile/selector expansion, parent authority, exact per-tool limits, and scope limits before constructing providers, handlers, clients, processes, or ADK agents. M3 will add host trust and effect decisions to that plan.

The complete plan lists, for every workflow scope, named-agent use, and step:

- source tool expression and trusted/workflow profile provenance;
- every profile expanded to exact names;
- exact final names in catalog-name byte order;
- capability and side-effect classes;
- symbolic resources and exact per-tool effective limits;
- exact run/activation/step call, iteration, timeout, artifact, and concurrency limits;
- the parent authority used for each subset check;
- trust/effect disposition and catalog/ceiling version or digest.

Text and schema-versioned JSON have the same semantics. They show final values rather than vague `inherited` markers and never reveal credentials, secret values, or concrete host roots. Runtime counters live beneath the immutable plan and cannot increase authority.

## ADK integration

Duto wraps native ADK rather than replacing it:

- family implementations use typed `functiontool.New` where applicable;
- a resolved exact set is exposed as an ADK `Toolset` or filtered toolset;
- a runner plugin `BeforeToolCallback` performs common admission/accounting;
- every handler repeats resource-specific checks at the actual I/O boundary;
- `platform.WithTaskRunner` supplies the run-level concurrency ceiling for model-issued parallel calls;
- a run-scoped read/write execution gate lets read-only calls share access but serializes mutation, process, and effect calls against every other call;
- static graph compilation rejects branches that could overlap when any participant has transitive mutation, process, or effect capability;
- ADK confirmation/HITL may be used for interactive local approval, but remote publication still follows ADR 003.

A callback is not an OS sandbox. Path confinement, redirect authorization, process groups, and Git invariants remain in the relevant handlers.

## Invocation contract

For every attempted call:

1. Identify the exact registered tool.
2. Check it is in the step's effective set.
3. Debit the call attempt atomically.
4. Validate categorical resources exactly.
5. Apply the earliest context/policy deadline.
6. Run the native ADK tool.
7. Bound or reject its result according to the family contract.
8. Emit redacted evidence through ADR 004.

Categorical violations, such as the wrong workspace, path, ref, command, domain, repository, or method, are rejected. Numeric requests may be clamped only within an already-approved category and the applied value is observable.

## Replacement behavior

M1 has no additive defaults, compatibility aliases, or permissive glob fallback. Omitted tools mean no tools. Invalid or unmatched selectors reject before construction. Tool families are constructed only after plan validation. The process tool is bounded but is never described as a sandbox.

Scenario implications:

- `no-tools`, `system-prompt`, and aggregation steps become naturally tool-free by omission.
- `file-exploration`, `git-history`, `prompt-from-file`, and `web-fetch` get exact current-family sets and bounds.
- `shell-exec`, `parallel-fan-in`, and process portions of `full-pipeline` remain bounded M1 `shell.run` compatibility; M3 may add fixed aliases without delaying process execution.
- `agent-skills` and `skills-injection` use ADK `skilltoolset` under the exact agent tool policy described in ADR 006.

## Deferred and rejected

- **Public tool plugins/MCP discovery:** no ranked v1 need; executable discovery would widen the trust surface.
- **Configuration for unimplemented future families:** add it with the implementation.
- **Recursive profiles and profile-owned limits:** obscure authority and create overlap precedence/counter ambiguity.
- **Capability-only workflow selectors:** duplicate the catalog vocabulary.
- **Arbitrary shell as the structured process path:** keep M1 compatibility visibly high risk and bounded; M3 may add fixed command aliases that narrow or replace it.
- **Silent declarative clamping:** reject author mistakes instead.

## Consequences

### Positive

- Omission is both safe and easy.
- The policy surface is much smaller than the previous four-layer schema.
- ADK owns tool execution and fan-out; duto owns authority.
- Future family configuration does not burden M1.

### Negative

- Workflows relying on additive defaults must migrate.
- Exact subset checks may reject configurations that previously happened to work.
- Tool callbacks are insufficient by themselves; handlers still need boundary checks.
