# ADR 008: CLI-first workflow runtime and delivery layers

- **Status:** Accepted; M1 and M2 shipped
- **Date:** 2026-08-26
- **Scope:** Product center, host-adapter relationship, planning boundary, milestone order, and disposition of durable-session decisions

## Context

Duto began as a CLI that compiles YAML workflows into ADK Go execution graphs. During the v1 architecture revision, GitHub-hosted durability requirements expanded into encrypted state branches, asynchronous reply correlation, cross-runner workspace recovery, lifecycle races, and effect replay.

Those are valid requirements for a future durable GitHub adapter, but they are not prerequisites for a useful workflow runtime. Treating them as core prerequisites would make the local CLI inherit GitHub-specific complexity and delay the smallest valuable product.

Duto also overlaps superficially with agent-process orchestrators such as Babysitter: both can describe dependent work, execute agents, retain progress, and expose checkpoints. Duto needs a narrower product boundary so it can learn from those systems without recreating them.

## Decision

Duto is a **host-neutral CLI and runtime for validating, inspecting, and executing bounded, typed AI workflow DAGs**.

A human, coding agent, planner, CI job, or other program may produce a workflow. Duto owns strict validation, static plan inspection, compilation to ADK Go, bounded execution, and typed results. Open-ended planning, backlog management, and arbitrary runtime graph creation sit above duto rather than inside its scheduler.

The primary interface is local and process-oriented:

```text
workflow YAML
    -> duto-ai validate / plan / run
    -> strict effective plan
    -> ADK Go workflow execution
    -> typed result and evidence
```

GitHub Actions remains a first-class official adapter. It maps trusted event inputs, permissions, summaries, outputs, and artifacts into the same core runtime. GitHub does not define portable workflow semantics, session identity, or the ordinary local execution path.

## Implementation status

The CLI-first M1 layer and the one-shot GitHub Action M2 layer are implemented. [ADR 010](010-m2-delivery-completion.md) records M2 completion without changing the sealed Action contract. M3, optional persistence, and durable-host behavior remain unimplemented and must not be inferred from the current binary or evidence bundle.

## Delivery layers

### Layer 1: Core workflow runtime

Required for the product to be useful:

- strict portable and trusted configuration;
- logical model aliases bound to trusted concrete targets;
- exact tool and workspace subsets;
- bounded prompt templates and typed inputs/outputs;
- static DAG validation and effective-plan inspection;
- compilation to native ADK agents and workflow nodes;
- enforceable limits, retry, timeout, and concurrency;
- local one-shot execution;
- deterministic typed results and redacted evidence.

A one-shot run uses in-memory ADK session/artifact services unless the caller explicitly selects another adapter. It does not need durable session identity, checkpoint replay, state-branch encryption, or cross-runner workspace reconstruction.

### Layer 2: Optional runtime capabilities

Added behind explicit core seams when justified:

- local filesystem-backed persistence;
- interactive pause/resume;
- durable artifact references;
- effect reconciliation and recovery;
- agent-oriented invocation and machine-readable planning output.

These capabilities must not enlarge the portable workflow's authority or make simple one-shot execution configure unused durability machinery.

### Layer 3: Host adapters

Official integrations around the core runtime:

- local interactive and headless CLI modes;
- GitHub Action event mapping, permissions, summaries, outputs, and artifacts;
- later GitHub durable sessions and asynchronous conversation resume;
- possible future automation hosts.

Host adapters own host identity, storage, event correlation, and credential mapping. Portable workflow YAML remains host-neutral.

## Planning boundary

Duto workflows may represent execution plans, and other agents may generate them. The v1 graph remains finite and statically inspectable before execution.

Duto does not initially become an autonomous planning harness. It does not maintain an open-ended backlog, rewrite its own graph during execution, create arbitrary agents, or recursively expand unbounded work. A planner may emit a complete duto workflow as a typed artifact; duto validates and executes it.

This gives a deliberate composition:

```text
human / Pi / Babysitter / planner
    -> produce or select a duto workflow
    -> duto validates and executes the bounded DAG
    -> caller consumes the typed result
```

## Relationship to Babysitter

Duto should learn from Babysitter's append-only execution journals, explicit task transitions, resumability, observability, operator checkpoints, and separation between process definition and executor.

It should not copy Babysitter's complete process-orchestration role. Babysitter coordinates long-running coding-agent processes; duto compiles portable typed workflow DAGs into ADK execution. Either may invoke the other where that composition is useful.

## Milestone order

### M1: CLI-first core

**Purpose:** deliver an independently useful local validator, inspector, and one-shot typed DAG runner.

**Entry:** accepted core contracts, the ADK Go v2.2 target, and provider-neutral fixtures.
**Exit:** local `validate`, `plan`, and one-shot `run` satisfy their exact stream/status contracts; invalid admission makes zero model/tool calls; typed results and redacted evidence are deterministic; all M1-owned current scenarios pass deterministic gates.
**Dependencies:** ADK Go v2.2 public behavior and admitted provider/tool adapters only.
**Excludes:** Action event/permission mapping, mutation/publication, durable state, pause/resume, replay, and asynchronous replies.

M1 includes strict host-neutral workflows, typed static DAGs, exact models/tools/prompts/results/limits, native declared subagents, and the currently shipped bounded file, read-only Git, GitHub read/review, web-fetch, and shell compatibility tools. Tool ownership follows authority rather than invocation host: local CLI callers may use trusted GitHub bindings, and `shell.run` is opt-in and bounded by trusted command, workspace, environment, time, output, and call ceilings.

### M2: First-class one-shot GitHub Action

**Status:** shipped; see [ADR 010](010-m2-delivery-completion.md).
**Purpose:** package the same one-shot JSON CLI contract as an official least-privilege Action adapter.
**Entry:** M1 complete and a fixed Action input/event/permission contract.
**Exit:** trusted event data maps to declared inputs; CLI status/result maps to summary, outputs, and artifacts; M2-owned scenarios and security checks pass without durable conversation state.
**Dependencies:** M1 and the GitHub Actions runtime.
**Excludes:** encrypted state branches, cross-runner recovery, asynchronous replies, and mutation/publisher authority.

### M3: Bounded authoring and safe outputs

**Status:** next milestone; implementation has not started.
**Purpose:** add admitted one-activation workspace/Git mutation and staged safe outputs.
**Entry:** M2 complete, fixed mutation/publisher policy, and deterministic negative security coverage.
**Exit:** workspace and Git guards enforce exact authority; agent jobs stage typed requests; a trusted publisher applies admitted effects; recovery artifacts remain bounded to one activation.
**Dependencies:** M1 core; M2 only for GitHub delivery paths.
**Excludes:** durable sessions, cross-activation effect replay, forbidden Git operations, and open-ended planning.

M3 may add hardened fixed-command aliases that narrow or replace M1 shell compatibility; process execution itself is not delayed until M3.

### Future durable-host milestone

**Purpose:** add explicit durable execution only when a host or use case demands it.
**Entry:** a separately approved product requirement, M1–M3 evidence, and exact references to retained durability decisions.
**Exit:** host-specific persistence, recovery, lifecycle, retention, and reply-correlation criteria are approved before implementation.
**Dependencies:** optional runtime-construction persistence seams; there is no reverse dependency from M1.
**Excludes:** becoming a prerequisite for ordinary local one-shot use.

This milestone owns local persistent pause/resume, encrypted GitHub state, cross-runner checkpoint/artifact/workspace recovery, lifecycle concurrency, retention and garbage collection, cross-activation effect replay, and asynchronous GitHub reply correlation.

No future durable-host contract is an M1 construction dependency.

## Decision disposition

Completed durable-session decisions are retained as future design inputs, not erased or treated as current core requirements.

| Decision | Disposition |
|---|---|
| ADK durable resume persistence boundaries | Retained for optional persistence adapters; not required by one-shot runs. |
| Host-neutral session identity and resume contract | Retained for future durable CLI/GitHub sessions. |
| GitHub state-branch constraints | Retained as research for the future GitHub durable adapter. |
| Encrypted GitHub state-branch model | Retained as the selected future GitHub storage security contract. |
| Checkpoint and replay guarantees | Retained for durable execution and effect recovery; one-shot runs need only ordinary failure/evidence behavior. |
| Portable model and reasoning syntax | Core M1 contract. |
| CLI surface, tool profiles, prompt/result, and milestone ownership | Core contracts are recorded in ADR 002, ADR 006, this ADR, and ADR 007's ownership matrix. |
| Workspace recovery, CLI resume, lifecycle concurrency, and GitHub reply protocol | Deferred together to the future durable-host milestone. |

The retained future-host decisions cover ADK resume persistence boundaries, protected GitHub state, host-neutral session identity, encrypted state envelopes, and checkpoint/replay guarantees. None is an M1 dependency. Before implementing durable hosting, promote the applicable decisions into public ADRs with current threat-model and runtime evidence.

## Consequences

### Positive

- The simplest valuable local workflow path drives the architecture.
- GitHub remains well-supported without leaking host details into the core contract.
- Agents can use duto as a bounded plan-execution format.
- Completed durability analysis remains available when the product reaches that milestone.
- M1 can deliver value without a session database, encrypted state branch, or distributed effect protocol.

### Negative

- Durable GitHub conversations arrive later.
- Some accepted future decisions will not receive implementation feedback during M1.
- The architecture must keep optional persistence seams narrow enough to add later without destabilizing the workflow contract.

## Rejected alternatives

- Make asynchronous GitHub conversation resume the organizing center of v1.
- Delete the durable-session decisions because they are no longer immediate.
- Turn duto into a general open-ended coding-agent process manager.
- Treat GitHub Actions as an incidental wrapper with no official integration contract.
