# ADR 007: Lean v1 contract synthesis

- **Status:** Accepted contract synthesis; milestone ordering is superseded by [ADR 008](008-product-center-and-delivery-layers.md)
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0 (`b264039aaec43baedc123e5b9a0cf87681d0bbca`)
- **Scope:** Canonical ownership across ADRs 001–006, use-case traceability, duto-test verification, and the historical M1 boundary

## Context

An earlier architecture pass was based on stale ADK v2.0 assumptions and recreated native v2.2 behavior for delegation, structured output, workflow validation/routing/retry/timeout, skills, HITL, and event capture.

This revision therefore:

- target ADK Go v2.2.0;
- use native ADK subagent tools rather than `agent.delegate`;
- support delegated context `fresh` and deterministic `snapshot` only;
- deliver clarification through the runtime-bound GitHub issue, PR, discussion, or equivalent conversation, then start a fresh run after a reply;
- aggressively simplify everything that is not a trust or data-loss protection.

This ADR synthesizes the revised contracts. It does not implement a milestone or create an implementation plan. The accepted delivery order is CLI-first M1, one-shot GitHub Action M2, bounded authoring M3, and a future durable-host milestone.

After this synthesis, the product was explicitly recentered on the local CLI and host-neutral DAG runtime. ADR 008 is canonical for product position, delivery layers, and milestone order. Detailed decisions here remain inputs where ADR 008 assigns them; GitHub durability and asynchronous resume are no longer core M1 prerequisites.

## Decision

The contract is accepted only after deterministic checks and independent review.

The architecture has one authority/execution direction:

```text
trusted runtime resources + portable workflow + trusted control evidence
        -> strict decode and pure effective-plan compilation
        -> model/tool/workspace/trust narrowing
        -> construct native ADK models, toolsets, agents, workflow nodes, and services
        -> run one ADK runner/workflow/session
        -> plugin projects redacted ADK events
        -> typed result and optional one-shot evidence bundle
```

No provider, client, tool handler, process, agent, ADK node, or remote adapter is constructed before the effective plan validates.

## Duto-owned versus ADK-owned

### Duto owns

- strict portable/trusted YAML and source diagnostics;
- logical model aliases and trusted concrete bindings;
- exact tool/workspace authority and runtime guards;
- exact M1 tool authority and resource policy;
- in M3, host trust-context derivation, remote-effect policy, and Git/GitHub mutation invariants;
- typed portable workflow declarations, outcomes, and source bindings;
- fresh/snapshot disclosure policy;
- redaction and public M1 result/evidence;
- in M3, staged operation manifests;
- rejection of removed syntax and compatibility paths.

### ADK owns

- `model.LLM` and model request/response streaming;
- `tool.Tool`/`Toolset`, function calls, and tool loops;
- agent modes, native subagent tools, `finish_task`, and RunNode dispatch;
- workflow graph scheduling, routes, joins, schemas, retry, timeout, concurrency, cancellation, HITL, and resume;
- session, artifact, and optional memory services;
- skill parsing/loading/toolsets;
- runtime callbacks/plugins and OpenTelemetry.

Duto wraps native behavior only where policy, redaction, source syntax, or public portability requires it. It does not mirror ADK lifecycle state.

## Canonical ownership matrix

| Concept | Canonical owner | Frozen invariant |
|---|---|---|
| Provider/model seam and trusted bindings | [ADR 001: Decision](001-provider-model-policy.md#decision), [Configuration ownership](001-provider-model-policy.md#configuration-ownership) | One consumer-owned resolver returns native `model.LLM`; workflows use logical aliases only. |
| Model lifecycle/errors/conformance | [ADR 001: Lifecycle](001-provider-model-policy.md#lifecycle), [Stable errors](001-provider-model-policy.md#stable-errors), [Adapter conformance](001-provider-model-policy.md#adapter-conformance) | Run-local construction/reuse preserves native schema, tools, usage, cancellation, and streaming. |
| Built-in tool identity and exact sets | [ADR 002: Built-in catalog](002-tool-catalog-configuration.md#built-in-catalog), [Source semantics](002-tool-catalog-configuration.md#source-semantics) | Registration grants no authority; omission or empty means no tools; every child remains inside parent authority. |
| Tool resources, plan, and guards | [ADR 002: Resource and limit policy](002-tool-catalog-configuration.md#resource-and-limit-policy), [Effective plan](002-tool-catalog-configuration.md#effective-plan), [Invocation contract](002-tool-catalog-configuration.md#invocation-contract) | Trusted roots, commands, and resources remain hidden; categorical violations reject at compile and I/O boundaries. |
| Future trust and remote effects | [ADR 003: Trusted-context derivation](003-trust-and-safe-outputs.md#trusted-context-derivation), [One typed safe-output contract](003-trust-and-safe-outputs.md#one-typed-safe-output-contract) | M3 owns host trust, mutation, and publication; M1 grants none of them. |
| Event capture and portable records | [ADR 004: ADK capture](004-execution-ledger.md#adk-capture), [Portable record](004-execution-ledger.md#portable-record) | One concrete plugin projects redacted M1 ADK events; later milestones add policy and effect facts. |
| Redaction, status, and projections | [ADR 004: Redaction and retention](004-execution-ledger.md#redaction-and-retention), [Deterministic status and outcome](004-execution-ledger.md#deterministic-status-and-outcome), [One stream, multiple projections](004-execution-ledger.md#one-stream-multiple-projections) | Thought and secrets are omitted; missing usage is absent; one fold drives the M1 result and bundle. |
| Native delegation and child definitions | [ADR 005: Decision](005-agent-delegation.md#decision), [Declared child contract](005-agent-delegation.md#declared-child-contract), [Native modes](005-agent-delegation.md#native-modes) | Declared ADK SubAgents become per-child tools beneath the sole root chat agent; there is no aggregate delegation protocol or nested runner. |
| Child context and authority | [ADR 005: Child context construction](005-agent-delegation.md#child-context-construction), [Authority and graph rules](005-agent-delegation.md#authority-and-graph-rules) | Only fresh or bounded snapshot context; the model cannot choose context, model, tools, workspaces, or budget. |
| Delegation scheduling, result, and lineage | [ADR 005: Parallel and sequential behavior](005-agent-delegation.md#parallel-and-sequential-behavior), [Results and failure](005-agent-delegation.md#results-and-failure), [Lineage and evidence](005-agent-delegation.md#lineage-and-evidence) | ADK owns dispatch and function responses; TaskRunner bounds safe fan-out; native IDs establish lineage. |
| Strict workflow syntax and schemas | [ADR 006: Strict decoding](006-workflow-v1-contract.md#strict-decoding), [Bounded schemas](006-workflow-v1-contract.md#bounded-schemas), [Portable root contract](006-workflow-v1-contract.md#portable-root-contract) | Versioned closed YAML compiles to native ADK schemas; only bounded Go templates are accepted, with no generic extras or public state keys. |
| Agents, steps, data, and routing | [ADR 006: Named agents and native modes](006-workflow-v1-contract.md#named-agents-and-native-modes), [Steps and bindings](006-workflow-v1-contract.md#steps-and-bindings), [Routing and terminal results](006-workflow-v1-contract.md#routing-and-terminal-results) | One typed object per step; explicit ancestor paths; fail-fast all-succeeded fan-in; native ADK mode placement. |
| Structured results, retry, limits, clarification | [ADR 006: Structured output and evidence](006-workflow-v1-contract.md#structured-output-and-evidence), [Retry, timeout, and failure](006-workflow-v1-contract.md#retry-timeout-and-failure), [Limits](006-workflow-v1-contract.md#limits), [Clarification](006-workflow-v1-contract.md#clarification) | Native OutputSchema, finish_task, and NodeConfig; terminal chat OutputKey extraction; enforceable limits; terminal awaiting-input result. |
| ADK compilation and fail-closed order | [ADR 006: Compilation to ADK](006-workflow-v1-contract.md#compilation-to-adk), [Validation order](006-workflow-v1-contract.md#validation-order) | All policy and source checks finish before construction; generated helper nodes do not form another scheduler. |

## Removed complexity

The revised design removes or defers:

- public provider factories/instances/catalog hierarchy;
- provider-specific portable `extra` values;
- future tool-family schema before the corresponding implementation;
- recursive/additive tool policy and mandatory empty scaffolding;
- a full mirrored lifecycle-event taxonomy and exported recorder interface;
- custom `agent.delegate`, dispatch modes, scheduler, and result aggregate;
- model-selected child aliases/context modes;
- custom final-text structured-output decoder;
- four fan-in policies, unbounded/general expression languages, and public state keys; bounded standard Go `text/template` remains accepted behind closed data/functions and strict bounds;
- hard aggregate-token claims without a tokenizer/counting contract;
- custom skill parser, HITL/resume protocol, artifact store, session store, and OTLP exporter.

M1 retains redaction, path/process/read-only Git guards, and zero-call rejection tests. Security-critical host trust and staged publication remain accepted M3 work.

## ADK v2.2.0 dependencies

The architecture relies on public v2.2 behavior that must be pinned by deterministic conformance tests:

- native SubAgents for `ModeSingleTurn`/`ModeTask`;
- `OutputSchema` with tools and task `finish_task`;
- workflow input/output schemas, routes, NodeConfig retry/timeout, joins, and cancellation;
- `platform.WithTaskRunner` for standard function-call fan-out;
- plugin run/event/model/tool callbacks;
- stable session event JSON and final streaming usage;
- `skilltoolset` restricted sources;
- ADK session/artifact services.

The shipped M1 implementation selects ADK v2.2.0 and pins the required public behavior with deterministic conformance tests.

ADK internals document a known caveat around synthetic single-turn input. Duto depends only on public behavior and captures regressions through model-request/event tests.

## Use-case traceability

| Selected contract | UC-1 issue to draft PR | UC-2 CI diagnosis/repair | UC-3 scheduled maintenance | UC-4 PR quality gate |
|---|---:|---:|---:|---:|
| Strict workflow and typed objects | Clarify/plan/build/verify | Diagnose/repair eligibility | Scan/no-action/remediation | Structured findings |
| Logical model aliases | Portable agent models | Portable diagnosis models | Portable scheduled analysis | Portable review models |
| Exact guarded tools | Role-specific reads/writes | Evidence/reproduce/repair | Scanner/web/process bounds | Read-only review |
| Trust and staged effects | Same-repo gated authoring | Fork-safe diagnosis | Trusted trigger, untrusted sources | Fork-safe staged review |
| Fresh/snapshot native delegation | Research/verify children | Evidence/diagnosis children | Optional later | Not required baseline |
| Plugin-backed evidence | Review/recovery result | Diagnosis/repair result | Source-backed report | Complete review evidence |
| Git invariants | One new draft-PR branch | Optional repair branch | Optional update branch | No merge/default write |
| GitHub clarification | Ask on originating issue/discussion | Ask on PR/check thread | Open/update issue when admitted | Ask on PR thread |

No selected contract exists for provider parity or framework feature count.

## Explicit deferred/non-goals

- shared transcript history, immutable full context forks, or model-generated summary context;
- dynamic/runtime-created agents, delegation cycles, persistent child conversations, or parallel writers;
- resumable duto workflow sessions for GitHub clarification;
- public provider/tool plugins, MCP discovery, remote A2A agents, or cross-run memory;
- general expression/template language, schema imports/registry, or dynamic graphs;
- merge, force, default/protected-branch writes, arbitrary refs/remotes, or cross-repository mutation;
- container/microVM sandbox claims;
- multi-PR decomposition or universal scanners/package managers;
- another provider chosen only for parity.

## duto-test verification contract

The accepted primitive and current-scenario ownership totals are recorded below. Detailed scenario assertions belong in the public `duto-test` suite rather than private planning artifacts.

Current repository agreement:

- retained primitive rows: 30 unique, with no missing, invented, duplicate, or unassigned IDs;
- repository commit: `3d3c3b84f8509cd249e6923c97bb6038ca79d0d4`;
- current scenario files: 18;
- current matrix rows: 18;
- difference in both directions: empty;
- duplicate ownership: empty;
- sorted name digest: `a9f4458fed938d5332fd75c5331d8dbedbc3c79a9905089cbf920e432afaca0a`.

Accepted ownership:

- CLI-first M1 owns all current local core scenarios except the two host-template migrations; this includes bounded currently shipped file, read-only Git, GitHub read/review, web-fetch, and `shell.run` compatibility plus native declared subagents;
- one-shot GitHub M2 owns `template-prompt-file` and `template-variables`, migrated from `.Event`/`.Env` access to declared typed inputs supplied by the Action adapter;
- bounded authoring M3 owns workspace/Git mutation and staged/direct publication, but no current scenario file uniquely exercises that accepted contract;
- no current scenario is assigned to the future durable-host milestone because the repository contains no durable-session scenario.

Tool ownership follows authority rather than invocation host. GitHub read tools may run from the local CLI with trusted bindings; M2 owns Action mapping and packaging. Bounded `shell.run` remains M1 compatibility under explicit trusted command, workspace, environment, time, output, and call ceilings. M3 may introduce hardened fixed-command aliases without making process execution an M1 omission.

Gate order remains:

1. `mise run check`
2. `mise run integration`
3. strict decode/effective-plan/zero-call security tests
4. deterministic fake-model/tool/ADK conformance tests
5. duto-test dry-run and repository security checks
6. live acceptance only after deterministic success

## Accepted successor boundary: `duto-cli-first-contracts`

### M1 objective

Implement the smallest independently useful CLI-first runtime: strict validation, complete effective-plan inspection, one-shot typed static DAG execution through ADK Go v2.2, exact tool authority and limits, bounded prompts, native declared subagents, and deterministic typed result/evidence output.

### M1 entry criteria

- CLI, tool-profile, prompt/result, and milestone contracts accepted.
- ADK Go v2.2 target/source assumptions independently reviewed.
- Public artifacts remain provider-neutral.

### M1 exit criteria

1. `duto-ai validate`, `plan`, and one-shot `run` satisfy the accepted process, stream, JSON, and status contracts.
2. Invalid source/policy/trust/data declarations construct or call no provider, model, tool, process, agent, ADK node, or remote adapter.
3. A finite static workflow executes through native ADK schemas/nodes and emits one typed terminal result plus redacted evidence.
4. Omitted tools mean no tools; every selected file/Git/GitHub/web/shell/subagent capability remains within trusted and parent ceilings.
5. Plugin-backed evidence deterministically projects result and diagnostics without raw thought, secrets, or fabricated usage.
6. The exact current-scenario matrix passes deterministic gates before dry-run/live acceptance.

### Out of M1

- GitHub Action event/permission/summary/output/artifact mapping (M2);
- workspace and Git mutation, publication, and trusted safe-output application (M3);
- durable pause/resume, encrypted state, cross-runner recovery, lifecycle races, cross-activation replay, and asynchronous replies (future durable-host);
- public plugin/MCP discovery, memory, dynamic graphs/agents, or release work.

Previously accepted durability decisions covering ADK resume boundaries, protected host state, session identity, encrypted state, and checkpoint replay remain future-host inputs. None is an M1 construction dependency.

## Verification

The CLI-first command, tool-profile, prompt/result, and milestone contracts were accepted on 2026-08-26. M1 is implemented in the current codebase and covered by build, unit, integration, scenario, CLI process, strict-decode, ADK conformance, and security-boundary tests. M2, M3, and durable hosting remain outside this ADR's implemented scope.

## Consequences

### Positive

- The design is materially smaller and easier to explain.
- ADK owns execution mechanics; duto owns policy, portability, and safety.
- Subagent context disclosure is explicit.
- GitHub clarification needs no session database.
- M1 implements only admitted current tool families and native bounded delegation; publication and durable-host machinery remain later.

### Negative

- The project must upgrade ADK before implementing the revised contracts.
- Shared/forked/summary context is unavailable in v1.
- GitHub replies create fresh runs rather than resuming old ones, and task children cannot remain paused across those runs.
- Native ADK behavior becomes a dependency that requires focused conformance tests.
