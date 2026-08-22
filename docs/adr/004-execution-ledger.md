# ADR 004: Execution evidence and result contract

- **Status:** Proposed revision
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0
- **Scope:** Redacted ADK event capture, duto policy/effect facts, deterministic result folding, and public evidence projections

## Context

ADK already owns agent, workflow, node, model, tool, session, cancellation, output, usage, and lineage events. V2.2.0 provides runner plugins, stable event JSON behavior, node paths, isolation scopes, requested input, and corrected cancellation/usage handling.

Duto must not build a second lifecycle runtime. It still needs a public evidence contract because raw ADK events may contain prompts, tool arguments/results, provider details, or other content unsuitable for logs and artifacts.

## Decision drivers

- ADK remains the execution source of truth.
- Redaction happens before duto persistence or export.
- Trust, policy, staged effects, and recovery facts absent from ADK remain visible.
- JSONL, result, summary, Action outputs, and optional telemetry cannot disagree on status.
- Missing usage remains absent rather than becoming zero.
- Private reasoning is never persisted.

## Alternatives

### A. Persist raw ADK sessions/events

Lowest implementation effort, but unstable as a public contract and unsafe for public evidence.

### B. Mirror every lifecycle transition in a duto event taxonomy

Portable but duplicates the ADK scheduler and risks status/lineage drift.

### C. Plugin-backed redacted projection

Observe ADK events through a runner plugin, emit a small portable record, and add only duto-owned policy/effect facts.

| Concern | A: raw ADK | B: full mirror | C: projection |
|---|---|---|---|
| Privacy | Weak | Strong | Strong |
| Runtime duplication | None | High | Low |
| Portability | Weak | Strong | Sufficient |
| Drift risk | Schema drift | Semantic drift | Bounded adapter |

## Decision

Select **C: one concrete plugin-backed evidence writer**.

Do not define an exported `Recorder` interface until a second writer exists. One concrete module owns normalization, redaction, sequencing, JSONL writing, result reduction, and manifest creation.

## ADK capture

Wire one runner plugin:

- `BeforeRunCallback` records the admitted run/effective-plan identity.
- `OnEventCallback` receives each ADK `session.Event` before duto persistence/export.
- model/tool error callbacks add safe normalized errors only when the terminal ADK event does not contain enough information.
- `AfterRunCallback` closes the projection after the ADK iterator is fully consumed.

Every duto run constructs a fresh ADK runner/plugin instance, unique session ID, run-scoped in-memory session and artifact services, and one evidence writer. Memory service is disabled in v1. Internal state/artifact keys are prefixed by run and node path. Raw session events are not exported or reused after redacted projection. The writer serializes callback access with one mutex/queue so concurrent callbacks cannot race sequence assignment or mix buffers.

The plugin preserves safe source correlation:

- ADK invocation and event IDs;
- node path and run ID;
- branch/isolation scope only when non-sensitive;
- function-call correlation IDs;
- output/result presence;
- reported usage;
- cancellation and error category.

It does not persist the raw event.

## Portable record

The public JSONL stream has a small closed kind set:

```go
type Kind string

const (
    KindRunStart       Kind = "run.start"
    KindADKEvent       Kind = "adk.event"
    KindPolicyDecision Kind = "policy.decision"
    KindArtifact       Kind = "artifact"
    KindSideEffect     Kind = "side_effect"
    KindRunFinish      Kind = "run.finish"
)
```

Each record contains:

```json
{
  "version": 1,
  "sequence": 12,
  "time": "2026-08-25T12:00:00Z",
  "run_id": "run-01",
  "kind": "adk.event",
  "source": {
    "invocation_id": "...",
    "event_id": "...",
    "node_path": "review/analyze@1"
  },
  "status": "succeeded",
  "payload": {}
}
```

Rules:

- Sequence is assigned by the single writer in observation order.
- Payload is a private per-kind concrete type; no open public `map[string]any` union is required.
- Duto does not synthesize parallel `agent.started`/`model.started` state machines when the ADK event already identifies the activity.
- Native node path, child tool call ID, and run ID are the lineage source. Duto adds logical workflow/agent/step names only as aliases resolved from the compiled plan.

## ADK event projection

`adk.event` contains only bounded portable facts needed by result reduction or diagnostics:

- logical step/agent/tool/model alias;
- event class (`model`, `tool`, `node`, `message`, `input_request`, `error`);
- status and normalized error kind;
- function-call/result correlation IDs;
- typed output digest or admitted small value;
- optional usage;
- retry/attempt ordinal when supplied by the workflow node;
- content/artifact references rather than large bodies.

Usage fields are optional:

```go
type Usage struct {
    InputTokens       *uint64 `json:"input_tokens,omitempty"`
    OutputTokens      *uint64 `json:"output_tokens,omitempty"`
    CachedInputTokens *uint64 `json:"cached_input_tokens,omitempty"`
}
```

A missing metric is absent. It is never encoded as a fabricated zero.

## Duto-owned facts

ADK does not know duto authority. Separate records cover:

- effective-plan and trust-policy decisions;
- rejected tool/model/workspace requests that did not reach ADK execution;
- artifact admission/rejection and manifest digests;
- staged/direct safe-output request, application, reconciliation, and disposition;
- recovery artifact creation;
- budget exhaustion when enforced outside an ADK node.

These facts reference the nearest ADK invocation/node/call ID where available. They do not create a parallel scheduler lineage.

## Redaction and retention

Classification happens before writing any sink.

| Data | Treatment |
|---|---|
| Private thought/reasoning parts | Omit |
| Credentials, tokens, raw environment, private endpoints | Omit |
| Concrete model/provider target | Omit; retain logical alias only |
| Prompt, issue/PR/discussion body, repository source | Omit from event stream or store as separately admitted content reference |
| Tool arguments/results | Emit bounded typed summaries or references; redact secret-shaped fields |
| Errors | Normalize kind; redact and truncate diagnostic text |
| Source patches/bundles | Separate artifact with purpose, digest, size, and access classification |
| Usage | Preserve reported optional numeric fields |
| IDs | Keep only runtime correlation IDs that reveal no secret/resource root |

Secrets are omitted rather than hashed. A secret hash is still a stable identifier and may enable guessing.

The writer applies per-record, total-record, total-byte, diagnostic, and artifact-reference ceilings. Exceeding evidence capacity cannot silently turn an incomplete run into success.

## Artifacts

Use ADK's artifact service for runtime artifact storage where it fits. Duto adds policy metadata required by public evidence:

- logical artifact name and purpose;
- media type;
- byte size and digest;
- producing step/node;
- retention class;
- admission/scan disposition;
- platform artifact reference when uploaded.

Artifact bytes are not copied into JSONL, summaries, Action outputs, or OTLP.

## Deterministic status and outcome

Execution status and domain outcome remain separate.

Statuses:

- `succeeded`
- `failed`
- `cancelled`
- `incomplete`

Fold order:

1. Context cancellation or ADK cancellation evidence -> `cancelled`.
2. Missing terminal ADK/result evidence -> `incomplete`.
3. Any unhandled step/node failure -> `failed`.
4. Otherwise -> `succeeded`.

Expected optional unavailability is represented as a successful typed domain outcome such as `completed_with_gaps`; v1 does not continue after an unhandled failed node.

Domain outcomes such as `completed`, `awaiting_input`, `diagnosis_only`, or `no_action` do not override execution status. A valid terminal clarification may therefore be `status: succeeded`, `outcome: awaiting_input`.

Staged means staged, never applied. A request artifact does not prove a remote effect occurred.

`failed`, `cancelled`, and `incomplete` produce a non-zero command result.

## Result schema

`result.json` is regenerated solely from the accepted record stream and compiled plan:

```json
{
  "version": 1,
  "run_id": "run-01",
  "workflow": "review",
  "status": "succeeded",
  "outcome": "completed",
  "started_at": "...",
  "finished_at": "...",
  "steps": [],
  "outputs": {},
  "artifacts": [],
  "usage": {},
  "errors": [],
  "recovery": []
}
```

Typed outputs remain bounded values validated by ADR 006. Large/source content is referenced, not embedded.

## One stream, multiple projections

All user-facing projections consume `result.json` and/or the redacted record stream:

- CLI progress on stderr;
- machine-readable result path/JSON on stdout;
- concise local `summary.md`;
- in M2, a GitHub step summary, content-free Action outputs such as status, outcome, run ID, result path, and failed step, and artifact upload;
- artifact manifest;
- optional ADK-native OpenTelemetry later.

A projector cannot reinterpret failure, convert staged to applied, or include content omitted by the writer.

M1 does not build a custom OTLP exporter. If ADK telemetry is enabled later, prompt/tool content capture remains disabled and portable result semantics still come from duto evidence.

## Evidence bundle

Minimum bundle:

```text
evidence/
  events.jsonl
  result.json
  summary.md
  manifest.json
```

Later milestones may add:

```text
  artifacts/
  recovery/
  safe-outputs/
```

`manifest.json` records version, run/effective-plan/workflow digests, file digests/sizes, completion state, and parent request linkage for staged publishers.

A complete manifest is written last. Missing or invalid manifest means evidence is incomplete.

## Failure and recovery behavior

- Every retry attempt retains its reported usage and normalized error.
- Cancellation stops scheduling and preserves already-observed evidence.
- A rejected required artifact prevents successful completion.
- Staged side effects record requested/staged state separately from later apply-run evidence.
- Publisher runs have their own run IDs and link to the verified request/manifest digest.
- Recovery patches/bundles are artifacts, not log text.

## Migration from current `WorkflowResult`

| Current behavior | V1 behavior |
|---|---|
| In-memory step map | Ordered step summaries derived from ADK node events |
| First failed step helper | Deterministic status fold plus normalized errors |
| Text/JSON/Markdown formatters | Projectors over one result |
| Output truncation in summaries | Bounded value or content reference |
| No retained execution evidence | One-shot JSONL/result/summary/manifest bundle; not a resumable checkpoint or session store |
| Logs mixed with output | Progress stderr; machine result stdout/path |

All 18 current `duto-test` scenarios must produce one valid result and manifest after migration. Exact scenario semantics remain in ADR 006 and the public scenario suite.

## Deferred and rejected

- Raw ADK session persistence as the public ledger.
- A second lifecycle state machine mirroring every ADK start/completion event.
- A speculative exported recorder hierarchy.
- Private chain-of-thought storage.
- Hosted observability backend.
- Cross-run conversation memory.
- Custom OTLP semantics that diverge from ADK telemetry.

## Consequences

### Positive

- Evidence becomes smaller and follows ADK instead of competing with it.
- Redaction and public result stability remain duto-owned.
- Optional usage is represented correctly.
- Plugins provide one capture seam for agents, models, tools, and workflow events.

### Negative

- The projection adapter must track the pinned ADK event surface.
- Some duto policy rejections occur before an ADK event and need explicit records.
- Raw ADK debugging data cannot be published as evidence.
