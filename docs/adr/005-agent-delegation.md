# ADR 005: ADK-native agent delegation

- **Status:** Accepted; M1 root-chat subset shipped
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0 (`b264039aaec43baedc123e5b9a0cf87681d0bbca`)
- **Scope:** Declared child agents, fresh/snapshot context, native dispatch, authority, bounds, failure, and lineage

## M1 implementation amendment

The shipped native integration executes a declared subagent tree only beneath a named `chat` agent used as the workflow's sole root and terminal step. Static `single_turn` named-agent steps cannot declare children, and `task` agents are child-only. Snapshot context on this root path supports declared workflow inputs and admitted files; ancestor-output snapshot sources reject because the root chat step has no ancestors. Persistent conversation and resume remain future-host work.

## Context

ADK v2.2.0 already exposes declared `llmagent.Config.SubAgents` as model-callable tools:

- `ModeSingleTurn` children become ordinary agent tools backed by `workflow.RunNode`;
- `ModeTask` children become task tools with an injected `finish_task` result mechanism;
- chat coordinators dispatch task children within the same runner/workflow/session infrastructure;
- `platform.TaskRunner` lets the host bound standard function-call fan-out.

The previous design added one generated `agent.delegate` tool, custom `single`/`parallel`/`chain` modes, a dispatcher result protocol, a dynamic wrapper, nested one-node workflows, and a separate concurrency executor. Those mechanisms duplicate current ADK behavior.

ADK does not, however, define product-level “forked” or “compressed” child context. Duto must state exactly what child content is disclosed.

## Decision drivers

- Use native ADK composition before adding orchestration code.
- The model may call only statically declared child tools.
- A child has a fixed logical model, tool set, workspace set, output schema, and context policy.
- Delegation never widens authority.
- Child prompt context is explicit, bounded, and reviewable.
- Parallel writers/process agents remain forbidden.
- Parent-child results are typed; raw child transcript/reasoning is not returned.

## Alternatives

### A. Native ADK subagent tools

Compile named children into `SubAgents`. ADK generates one tool per child and owns RunNode dispatch, task completion, function responses, cancellation, and event lineage.

### B. One aggregate `agent.delegate` tool

Duto defines child selection, batch/chain modes, scheduler, result aggregation, and dispatch lineage.

### C. `tool/agenttool.New`

Wrap a child in an agent tool that starts a separate in-memory runner/session and collapses child events into one result.

| Concern | A: native SubAgents | B: aggregate tool | C: agenttool |
|---|---|---|---|
| Same ADK runner/session | Yes | Yes if carefully wrapped | No |
| Custom scheduler/protocol | No | Yes | Hidden nested runner |
| Typed child tools | Native | Generated union | Native-ish |
| Event lineage | Native node/call IDs | Custom mapping | Collapsed |
| Context control | Duto policy over native modes | Duto-owned | New isolated session |
| Complexity | Small | Large | Small but wrong lifecycle |

## Decision

Select **A: native ADK `SubAgents`**.

Continue rejecting `agenttool.New` for workflow delegation because it creates another runner/session and loses the selected event/lifecycle semantics.

Delete from the selected contract:

- `agent.delegate`;
- `DispatchRequest`/`DispatchResult`;
- public dispatch modes `single`, `parallel`, and `chain`;
- model-selected child model aliases;
- custom dynamic dispatch wrappers;
- one-node nested workflows unless a conformance test proves a missing native behavior;
- a separate delegation fan-out executor.

## Declared child contract

Each named child fixes:

- unique name and description;
- `mode: single_turn` or `mode: task`;
- one logical model alias;
- instruction;
- exact tools and workspaces;
- bounded input and output schemas;
- `context.mode: fresh` or `snapshot`;
- optional declared snapshot sources;
- call, depth, input/snapshot/result limits;
- finite allowed subagents.

Example:

```yaml
agents:
  researcher:
    description: Gather bounded evidence.
    mode: single_turn
    model: light
    instruction: {text: Return cited findings.}
    tools: [web.fetch]
    workspaces: []
    context: {mode: fresh}
    input:
      type: object
      properties:
        question: {type: string, max_length: 2048}
      required: [question]
    output:
      type: object
      properties:
        findings:
          type: array
          max_items: 20
          items: {type: string, max_length: 4096}
      required: [findings]

  coordinator:
    description: Coordinate analysis.
    mode: task
    model: capable
    instruction: {text: Delegate research, then produce a decision.}
    tools: []
    subagents: [researcher]
```

The compiler constructs both with `llmagent.New` and passes `researcher` in the coordinator's `SubAgents`. ADK installs the `researcher` tool automatically.

The child model alias is fixed by the declaration. If a use case truly needs two model variants, it declares two reviewable child definitions with distinct names; the model cannot select a concrete target or silently upgrade authority at call time.

## Native modes

### `single_turn`

Use for a bounded delegated job whose result can be produced within one ADK agent invocation.

ADK behavior used:

- ADK's single-turn workflow-node wrapper forces `IncludeContentsNone`;
- explicit function arguments become the child input;
- input/output schemas define the generated tool;
- the child may use its own admitted tools through the normal ADK loop;
- final output is validated through `OutputSchema`/node output validation;
- independent read-only single-turn calls may run concurrently under `platform.TaskRunner`.

### `task`

Use when the child needs a multi-round task/tool loop and must explicitly call `finish_task` with a typed result.

ADK task children are dispatched by the chat coordinator through `RunNode` with stable call IDs and isolation scopes. Native task tools are sequential; their declaration explicitly warns against parallel use.

V1 does not leave a task child waiting for another user turn. A child needing clarification finishes with a typed `awaiting_input` result, which the coordinator may surface through ADR 003. Persistent delegated conversations remain deferred.

## Child context construction

Scheduling and context are separate concepts. V1 supports two context modes.

### Fresh

`fresh` means:

- child instruction and admitted skills/static context;
- current typed child-tool arguments;
- the child's own function-call/result history during that invocation;
- no parent or sibling conversation transcript.

It does **not** mean a separate process or security sandbox. Children share only the current run's ADK infrastructure. Every duto run creates a fresh session ID, run-scoped artifact adapter and evidence plugin, disables memory, namespaces internal state/artifacts by run and child path, and disposes raw session events after redacted projection. Tool/workspace/state guards remain mandatory.

Implementation uses native `IncludeContentsNone`, `ModeSingleTurn`, task isolation scopes, and sub-branches as appropriate. Conformance tests verify that parent/sibling text, state, artifacts, and plugin buffers do not enter another child or run.

### Snapshot

`snapshot` adds a deterministic, bounded block built from declared workflow inputs and admitted files. The root-chat placement has no ancestor output, and M1 has no portable artifact source.

Example:

```yaml
context:
  mode: snapshot
  include:
    - input: objective
    - file: {workspace: source, path: README.md, max_bytes: 32768}
```

Duto compiles the declaration to an ADK `InstructionProvider`/input builder that reads only the named values and labels each source. The snapshot is produced at child invocation time, before the model call, with:

- canonical source order;
- source kind/name and digest;
- per-source and aggregate byte bounds;
- redaction/content-reference rules from ADR 004;
- no credentials, concrete roots, provider data, raw thought, or undeclared session state.

The compiler proves the root chat activation can access every declared source. If it cannot, the value must be supplied as a normal typed child input or the declaration is rejected.

Snapshot content is untrusted prompt data. Provenance does not grant authority or guarantee correctness.

### Deferred modes

- `shared_history`: ADK `IncludeContentsDefault` exposes relevant session history, not an immutable or independently reviewable fork.
- `summary`: requires a separate summarizer call, prompt, model alias, budget, provenance, validation, and failure policy.
- full context-window fork: ADK has no public immutable context-clone primitive.

These modes are not accepted v1 strings. Add one only when a ranked use case proves fresh/snapshot insufficient.

## Model-facing interface

The model sees one ordinary tool per admitted child. Its schema is the child's declared input schema.

It cannot express:

- provider/concrete model target;
- tools or workspace profile;
- context mode or snapshot source list;
- credentials, endpoint, root path, or state key;
- effect mode;
- timeout, call limit, depth, or concurrency override;
- arbitrary agent name.

Unknown child calls remain unknown ADK tools and are rejected. Duto validates the complete declared subagent graph before constructing the parent.

## Authority and graph rules

1. Every child is declared before execution.
2. Every parent lists its allowed direct subagents.
3. The graph is acyclic.
4. Maximum depth and total child-call counts are finite runtime ceilings.
5. A child's direct tools/workspaces/model are independently compiled; they are not inherited from the parent's direct set.
6. The parent's compiled transitive delegation envelope includes every allowed child's direct and transitive authority and must remain within trusted workflow ceilings.
7. A parent cannot call a child's tools directly unless separately admitted; permitting a child does not add its tools to the parent toolset.
8. Context text and snapshot sources cannot widen capability.
9. Unknown, disallowed, or exhausted calls fail before the child model/tool boundary.

## Parallel and sequential behavior

There is no public scheduling-mode argument.

- One child tool call is one delegation.
- Multiple independent `single_turn` child calls emitted in one model response use ADK's normal parallel function-call path.
- Duto installs a bounded `platform.TaskRunner` on the invocation context.
- Parallel delegation is admitted only for transitively read-only single-turn leaves with no process, mutation, remote effect, nested delegation, or HITL.
- `task` children and all writers/process agents execute sequentially.
- A chain is expressed by the coordinator making another child call after receiving and validating the previous function response.
- Parallel result/correlation order follows ADK function-call order and is pinned by conformance tests.
- Parent cancellation propagates through ADK and stops queued/running work.

The TaskRunner controls scheduling, not authority. Duto admission counters still enforce total calls, depth, and child-specific limits.

## Results and failure

- Child input uses native ADK `InputSchema`.
- `single_turn` output uses native `OutputSchema` and node validation.
- `task` output uses native `finish_task`, whose declaration follows the output schema.
- A root `chat` coordinator is terminal: duto assigns a private run-namespaced `OutputKey`, reads the final schema-validated value after the iterator completes, and does not route that value to another workflow node.
- Parent receives the normal function response, not the raw child transcript.
- Large/source outputs become artifact/content references under ADR 004.
- Invalid child output is a tool/delegation error, not text success.
- A coordinator may handle a failed child and produce a declared domain outcome; otherwise the containing step fails.
- There is no implicit fallback to another child/model or broader tool set.
- Retry uses the containing ADK `NodeConfig` and ADR 006 policy; mutation-capable children are not automatically retried.

## Budgets

Duto keeps only limits ADK does not provide as product policy:

- total child calls;
- maximum depth;
- standard function-call concurrency through TaskRunner;
- the parent/workflow context deadline shared by native child dispatch;
- maximum input/snapshot/result bytes;
- inherited workflow model/tool/artifact ceilings.

ADK usage events remain the token evidence source. Missing usage is absent. V1 does not claim exact pre-call aggregate-token enforcement without a provider tokenizer/counting contract.

## Lineage and evidence

ADR 004 preserves:

- parent function-call ID;
- native child tool name;
- ADK invocation/event IDs;
- node path/run ID/isolation scope where safe;
- logical parent/child names from the compiled plan;
- status, output reference, usage, and normalized error.

Do not create a second delegation lineage graph when native IDs and the compiled plan establish the relationship.

## ADK v2.2.0 conformance gates

Before M1 claims native delegation support, deterministic tests pin:

1. `SubAgents` expose only declared child tools.
2. `ModeSingleTurn` receives no parent/sibling history.
3. `ModeTask` dispatches within the same runner and returns typed `finish_task` output.
4. `OutputSchema` works with child tools.
5. Fresh and snapshot prompts contain exactly admitted sources.
6. TaskRunner never exceeds the configured parallel cap.
7. Parallel writers/process agents are rejected before construction.
8. Call order, parent-deadline cancellation, errors, and usage survive native event projection.
9. Child/sibling/run state, artifacts, raw events, and plugin buffers do not leak across scopes.
10. No `agenttool.New` nested session appears.
11. Known ADK wrapper implementation caveats do not violate duto's public context/output contract.

## M1 delegation conformance scenarios

| Scenario | Deterministic assertion |
|---|---|
| `agent-delegation-catalog` | Only declared native child tools exist; unknown/cyclic child rejects before model call. |
| `agent-delegation-context` | Fresh excludes parent history; snapshot includes exactly declared sources/digests. |
| `agent-delegation-tool-isolation` | Parent, siblings, and child expose independent exact tool/workspace sets. |
| `agent-delegation-concurrency-cap` | Read-only single-turn fan-out respects TaskRunner; writer absent from parallel plan. |
| `agent-delegation-lineage` | Native call/node/event IDs link parent and child exactly once. |
| `agent-delegation-failure` | Invalid output, parent-deadline cancellation, and handled/unhandled failure propagate correctly. |

## Rejected alternatives

- Custom aggregate `agent.delegate` protocol.
- `tool/agenttool.New` nested runner/session.
- Model-selected context mode, model alias, authority, or budget.
- Shared-history described as a fork.
- Summary mode without a ranked use case and separate budget/provenance contract.
- Runtime-created agents, cycles, persistent delegated conversations, or parallel writers.

## Consequences

### Positive

- Most of the previous ADR is deleted in favor of native ADK behavior.
- Child tools, task completion, cancellation, and lineage follow the selected runtime.
- Fresh/snapshot context semantics are explicit and testable.
- The model cannot choose disclosure or authority characteristics.

### Negative

- Native task children are sequential.
- Snapshot construction still requires a small duto input/instruction adapter.
- ADK v2.2.0 must be upgraded and conformance-tested before implementation relies on these behaviors.
- Summary/full-fork features remain deferred.
