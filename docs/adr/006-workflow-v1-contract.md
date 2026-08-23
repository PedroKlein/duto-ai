# ADR 006: Lean v1 workflow contract

- **Status:** Accepted; CLI-first M1 implementation shipped
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0 (`b264039aaec43baedc123e5b9a0cf87681d0bbca`)
- **Scope:** Strict portable YAML, trusted runtime ownership, typed data, static graphs, native ADK agents/workflows, outcomes, clarification, retries, and limits

## Context

V1 needs strict portable workflows without recreating ADK's runtime. M1 executes each workflow as a fresh in-memory one-shot run. Persisted state, resume, checkpoint replay, and asynchronous reply correlation are retained future-host concerns, not portable workflow fields or M1 dependencies. ADK v2.2.0 already supplies:

- `llmagent` chat/task/single-turn modes and native subagent tools;
- agent `InputSchema`/`OutputSchema`, including structured output with tools;
- workflow node input/output validation;
- static edges/routes, joins, parallel workers, retries, timeouts, and concurrency;
- dynamic child dispatch, cancellation, requested input, persisted run state, and resume;
- sessions, artifacts, skills, plugins, and telemetry.

Duto owns strict YAML, portable aliases, authority, trust, source diagnostics, and the mapping from user-facing data to those native interfaces.

The prior schema exposed separate output ports, result envelopes, four fan-in modes, generic extras, comprehensive token budgets, custom delegation modes, and large Go pseudo-types. The approved use cases do not justify that surface.

## Design alternatives

### A. Agent-first catalog

Top-level named agents and explicit node kinds describe every graph node. Good for reuse, verbose for ordinary workflows.

### B. Step-first typed workflow

A source-ordered `steps` list remains the graph. One-use agents stay inline; named agents exist only for reuse or native delegation. Each step consumes and emits one typed object.

### C. ADK node YAML

Expose `AgentNode`, `JoinNode`, `DynamicNode`, state keys, and native options directly. This is powerful but couples the portable contract to ADK implementation vocabulary.

| Concern | A: agent-first | B: step-first | C: ADK node YAML |
|---|---|---|---|
| Simple workflow | Verbose | Small | Framework-heavy |
| Reusable/delegated agent | Strong | Strong when needed | Strong |
| Static inspection | Strong | Strong | Strong but runtime-coupled |
| Replacement of the prior contract | Medium | Best | Poor |
| Interface size | Medium | Smallest | Large |

## Decision

Select **B: a step-first typed workflow compiled directly to native ADK agents and workflow nodes**.

Duto does not add another scheduler, agent loop, session database, schema engine, retry engine, or HITL protocol.

## Two strict documents

### Trusted runtime document

Trusted configuration binds concrete resources:

```yaml
version: 1
providers:
  default:
    type: built-in
    config:
      credential: "${MODEL_CREDENTIAL}"
models:
  light: {provider: default, target: model-a}
  capable: {provider: default, target: model-b}
workspaces:
  source: {root: "${DUTO_WORKSPACE}", access: read}
evidence:
  directory: "${DUTO_EVIDENCE_DIRECTORY}"
```

It owns:

- provider adapters/configuration, credentials, endpoints, and concrete targets;
- workspace roots/provenance and maximum access;
- centrally reusable flat tool profiles, tool-family ceilings, and fixed command/resource bindings;
- the optional one-shot evidence directory.

Host trust-context policy and publication admissions are M3 configuration, not accepted M1 fields.

### Portable workflow document

Portable YAML owns:

- logical model aliases;
- instructions, admitted skills/context, and typed input/output;
- exact tool/workspace subsets;
- named/inline agents and declared native subagents;
- static steps/dependencies/routes;
- outcomes, result projection, retry, failure, and finite run limits.

It never contains concrete provider targets, credentials, host roots, fixed executable paths, or direct/staged authority fields.

## Strict decoding

Both documents require integer `version: 1`.

Before semantic validation, reject:

- unknown fields at every level;
- duplicate mapping keys;
- aliases, anchors, and merge keys;
- explicit YAML null;
- unsupported tags;
- invalid UTF-8;
- numeric overflow, NaN, infinity, or scalar coercion;
- multiple YAML documents;
- environment expansion in portable workflows.

Trusted adapter-owned scalar values may expand environment variables only after structural decoding. Expanded values are never copied to diagnostics or evidence.

Errors retain file, line, column, and field path. A missing version is rejected rather than guessed as another schema.

## Names

- Workflow, agent, step, profile, skill, workspace, and result-property names: `[a-z][a-z0-9-]{0,62}`.
- Tool names follow ADR 002's dot-separated grammar.
- Domain outcomes: `[a-z][a-z0-9_]{0,62}`.
- Names are case-sensitive ASCII.

## Bounded schemas

V1 supports the subset needed by ADK `genai.Schema` and workflow `jsonschema` validation:

- `object`
- `array`
- `string`
- `integer`
- `number`
- `boolean`

Example:

```yaml
input:
  type: object
  properties:
    objective: {type: string, max_length: 4096}
  required: [objective]
output:
  type: object
  properties:
    outcome: {type: string, enum: [completed]}
    report: {type: string, max_length: 16384}
  required: [outcome, report]
```

Rules:

- Objects are closed: undeclared properties reject.
- Object nesting, property count, and encoded bytes are bounded by trusted limits.
- Arrays require `items` and finite `max_items`.
- Strings require finite `max_length`.
- Numeric bounds are finite when the value can influence allocation or control flow.
- `required` is a subset of `properties`.
- `outcome` is required on every agent/step output and its enum is finite.
- No recursive schemas, external references, arbitrary JSON Schema, coercion, or open additional properties.

Duto strict-decodes the source subset, then compiles it to native ADK agent and workflow schemas. ADK performs runtime input/output validation.

## Portable root contract

```yaml
version: 1
name: example

description: Example workflow.

inputs:
  objective:
    schema: {type: string, max_length: 4096}

model: capable
model_config:
  temperature: 0.2
  max_output_tokens: 4096

tool_profiles: {}
skills: {}
agents: {}

limits:
  timeout: 10m
  max_iterations: 20
  max_model_calls: 20
  max_tool_calls: 50
  max_concurrency: 2
  max_parallel_calls: 4
  max_artifact_bytes: 8388608

steps:
  - id: terminal-step
    needs: []
    instruction: Return the objective.
    tools: []
    workspaces: []
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 4096}
      required: [outcome, report]
result: {step: terminal-step}
```

Required fields are `version`, `name`, `model`, `limits`, `steps`, and `result`. `steps` must contain at least one step. Optional collections may be empty where their field contract allows it. Description, inputs, model config, profiles, skills, and named agents are optional.

The workflow model is the default logical alias. An agent or step may select another trusted alias explicitly; it cannot select a concrete target. There is no separate allowed-alias list. The finite set is the aliases actually referenced by the validated workflow and present in trusted runtime configuration.

Portable model config contains only `temperature` and `max_output_tokens`. Provider-specific workflow extras are deferred.

## Instructions, files, context, and skills

An `instruction` is exactly one closed source alternative. A scalar is the concise literal form:

```yaml
instruction: Review the supplied input.
instruction: {text: Review the supplied input.}
instruction:
  file: {workspace: source, path: prompts/review.md, max_bytes: 32768}
instruction:
  template:
    text: 'Review: {{ .Step.Inputs.objective }}'
    max_output_bytes: 32768
instruction:
  template:
    file: {workspace: source, path: prompts/review.tmpl, max_bytes: 32768}
    max_output_bytes: 65536
```

The exact union is a YAML string, `{text: string}`, `{file: FileSource}`, or `{template: {text: string, max_output_bytes: n}}` / `{template: {file: FileSource, max_output_bytes: n}}`. `FileSource` is exactly `{workspace, path, max_bytes}`. All records are closed; discriminators are mutually exclusive; bounds are positive and may only narrow trusted inline, file, parse, rendered-output, and final-prompt ceilings. Extensions never infer behavior.

Named-agent and inline-step `instruction` fields accept every source form. The workflow root has no instruction because it is not an agent. A step using `agent:` cannot override that named agent's instruction. Descriptions, schemas, routes, tool arguments, context, and skills are never templates.

Prompt files are normalized relative regular UTF-8 files beneath an admitted readable workspace. Traversal, symlink escape, inaccessible/non-regular files, invalid UTF-8, and excessive bytes reject. `validate` and `plan` read and parse referenced files during admission. Each invocation freezes the bytes in its immutable effective plan; `run` does not reread them after execution begins.

Templates use the full standard Go `text/template` syntax: actions, conditionals, ranges, variables, pipelines, parentheses, comments, and acyclic local named templates. Duto does not invent another expression language. The fixed function set is `and`, `or`, `not`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `len`, `index`, `slice`, `print`, `printf`, `println`, `json`, and `quote`. `call`, HTML/JS/URL helpers, functions or methods in data, arbitrary registries/plugins, cross-source inclusion, and direct or indirect named-template recursion reject.

Each activation receives exactly:

- `.Workflow.Name` and `.Workflow.Inputs`: the workflow name and closed validated declared workflow-input object;
- `.Step.ID` and `.Step.Inputs`: the step ID and exact closed validated object built through `with`;
- `.Predecessors`: already validated output objects for direct `needs` predecessors only, keyed by exact step ID;
- `.Runtime.RunID` and `.Runtime.Attempt`: bounded provider-neutral runtime-owned metadata. RunID is an opaque one-shot identifier, not a session/resume token.

No host adapter may add roots or keys. Environment, secrets, credentials, endpoints, provider/model metadata, host events, arbitrary filesystem/network access, clocks, randomness, transcripts, memory/session services, ADK state keys, artifact bytes, tool registries, and plugin registries are unavailable. Host data may enter only as declared typed workflow input through the ordinary adapter seam.

Template execution uses `missingkey=error`. Direct interpolation accepts strings, integers, finite numbers, and booleans with deterministic JSON-compatible formatting. Objects/arrays require canonical compact `json`; `quote` emits a JSON string. Strings are inserted verbatim with no automatic HTML, shell, YAML, or Markdown escaping. File bytes and non-trimmed template whitespace are preserved; Go trim markers retain standard semantics. Rendered data is never reparsed.

Parsing is bounded by source bytes, actions, parse nodes, syntax/lookup depth, named-template count, and an acyclic call-depth ceiling. Typed input schemas bound strings, arrays, objects, and nesting. Execution has an operation budget and a limit writer that aborts before retaining or sending bytes beyond `max_output_bytes` or the trusted final-prompt ceiling.

Missing/unknown data, wrong types, invalid indexes, unknown functions/fields, invalid syntax, forbidden capabilities, inaccessible/oversized files, and rendered overflow are deterministic errors. Static/source errors are admission failures with zero model/tool calls; activation-dependent render errors fail the step and produce no output. Invalid templates never pass through as literal text.

Skills continue to use ADK v2.2.0 `skilltoolset` through a duto restricted `skill.Source`:

```yaml
skills:
  go-review: {workspace: source, path: .agents/skills/go-review}
```

An agent selects exact skill names. The source exposes only selected skills/resources and enforces workspace/path/byte ceilings. Skill `allowed-tools` metadata may narrow advice but never widens ADR 002 authority. Static context files use the same bounded source rules. Delegated context uses ADR 005 `fresh` or deterministic `snapshot`; shared transcript and summary modes are invalid in v1.

## Tool and workspace semantics

ADR 002 is canonical. Trusted configuration and portable workflows may define uniquely named, flat selector-only profiles. Profiles cannot reference profiles or own limits. A portable scope references a central or local profile only through an explicit tool expression.

The common exact-set form is concise:

```yaml
tools: [files.read, git.read.diff]
```

Composition uses the closed expanded form:

```yaml
tools:
  from: parent
  add_profiles: [source-review]
  add: [git.read.log]
  remove_profiles: [history]
  remove: [files.grep]
```

- The array normalizes to `from: empty` plus `add`.
- Omitted tools, `tools: []`, and an empty expression mean no direct tools at workflow, named-agent, and step scopes.
- Inheritance is explicit through `from: parent`; it is invalid at workflow root.
- Expansion order is base, add profiles, add selectors, remove profiles, remove selectors; overlaps deduplicate by exact name and each removal must match the current set.
- Exact names and allowed terminal namespace wildcards must match. M1 allows `files.*`, `git.read.*`, and `github.read.*`; `git.*`, `github.*`, and portable global `*` are invalid.
- A workflow is bounded by trusted authority. Every top-level agent, delegated agent, and step is a subset of its use-specific parent/orchestrator authority.
- A child's tools never become directly callable by its parent merely through delegation.
- Per-tool limits are separate exact-name `tool_limits`; profiles never carry call counts or other limits.
- Omitted workspaces means no workspace access; trust may further remove authority or force staged operation handling.

Unknown profiles/tools, unmatched or malformed selectors, invalid removals, and any trusted/parent widening are deterministic admission failures with zero model/tool calls.

## Named agents and native modes

Named agents exist for reuse or delegation:

```yaml
agents:
  reviewer:
    description: Review bounded source changes.
    mode: single_turn
    model: capable
    instruction: {text: Return structured findings.}
    tools: [files.read, git.read.diff]
    workspaces: [{name: source, access: read}]
    skills: [go-review]
    context: {mode: fresh}
    input:
      type: object
      properties:
        objective: {type: string, max_length: 4096}
      required: [objective]
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        findings:
          type: array
          max_items: 50
          items: {type: string, max_length: 2048}
      required: [outcome, findings]
```

Modes map directly to ADK:

- `single_turn`: static workflow node or delegated bounded job;
- `task`: delegated task child only; returns through native `finish_task` and may not remain waiting for another user turn;
- `chat`: root coordinator only, directly reachable from workflow start; may own declared subagents.

These restrictions mirror ADK workflow validation. Task agents are not static graph nodes, and chat agents are not placed after another node. In the shipped M1 compiler, any named agent with subagents must be reached through the sole root and terminal `chat` step. A static `single_turn` named-agent step cannot declare children. A root chat step has `needs: []`; its `with` values may use workflow inputs/literals only and are encoded as the invocation's user content rather than a predecessor-node output.

A named agent with subagents lists them exactly:

```yaml
subagents: [researcher, verifier]
```

ADR 005 controls context, graph acyclicity, parallel-safety, limits, and lineage. There is no `agent.delegate` field or dispatch-mode schema.

## Steps and bindings

A step is either inline or references a named agent.

Inline:

```yaml
- id: inspect
  needs: []
  instruction: {text: Inspect the objective.}
  model: light
  tools: []
  workspaces: []
  input:
    type: object
    properties:
      objective: {type: string, max_length: 4096}
    required: [objective]
  with:
    objective: {input: objective}
  output:
    type: object
    properties:
      outcome: {type: string, enum: [completed]}
      findings: {type: string, max_length: 8192}
    required: [outcome, findings]
```

Named:

```yaml
- id: review
  needs: [inspect]
  agent: reviewer
  with:
    objective: {output: {step: inspect, path: [findings]}}
```

Each `with` property is exactly one of:

```yaml
value: {input: objective}
value: {output: {step: inspect, path: [findings]}}
value: {literal: concise}
```

Rules:

- Output sources must be ancestors.
- `path` contains declared object-property names only.
- Source and target schemas are structurally assignable without coercion.
- A required source must exist on every activation path.
- `optional: true` is allowed only on an output-property binding whose target property is not required; it permits that property to be absent from a successfully validated source object and never masks a failed source step.
- There is no implicit predecessor map, last-step output, JSONPath, transform language, fallback, concatenation, or state-key access.
- Source order, not completion order, determines fan-in object construction.

The compiler emits small ADK function/join nodes to build typed input objects; those nodes do not form a second scheduler.

## Dependencies and fan-in

`needs` is explicit. V1 supports only `all_succeeded`: every dependency must succeed before the step runs. Any unhandled predecessor error fails the workflow. Expected optional unavailability is returned as a successful typed domain outcome, not a failed node.

All-terminal failure continuation and other fan-in policies are deferred until ADK exposes the needed routing semantics or a ranked use case justifies one explicit error-to-data adapter.

Static graph cycles, duplicate IDs, missing dependencies, invalid ADK mode placement, and impossible typed bindings reject before construction.

## Routing and terminal results

`when` remains a conjunction over direct dependencies and may inspect only declared bounded output values. A false predicate marks the step skipped.

Top-level `outcomes` and `finish` are replaced by one required closed `result` union. The linear common case names the sole reachable terminal step:

```yaml
result: {step: report}
```

Every finite value of that step's required output `outcome` enum is covered, and the workflow terminal schema is exactly its closed output schema. The terminal root-chat coordinator uses the same form; its private OutputKey projection remains an implementation detail.

Branching uses routes:

```yaml
result:
  routes:
    - when: {step: clarify, outcome: awaiting_input}
      step: clarify
    - when: {step: plan, outcome: completed}
      step: plan
```

Each route is exactly `{when: {step, outcome}, step}` and both step values must match. There is no `as` and no duplicate workflow outcome list. The workflow outcome is the validated result object's required `outcome`; the workflow result is that complete already validated step output.

Static validation requires exactly one route for every reachable terminal `(step, outcome)` pair. Outcomes consumed by a satisfiable successor are not terminal. Unknown, duplicate, unreachable, or uncovered pairs reject. Routes sharing an outcome name must have structurally identical output schemas or reject as ambiguous. A route cannot become eligible while an unrelated branch remains runnable.

Selection occurs only after successful graph quiescence. Successfully validated terminal outputs are examined deterministically in source order, but order is not a tie-breaker: exactly one eligible pair must exist. Zero is `result_uncovered`; more than one is `result_ambiguous`. A skipped terminal supplies no candidate. A failed terminal has no output and fail-fast execution wins; expected unavailability remains a successful typed domain outcome. Static coverage/reachability/schema errors are admission failures; runtime uncovered/ambiguous results are execution failures. Domain outcome never overrides ADR 004 execution status.

## Structured output and evidence

Duto sets native ADK `llmagent.Config.OutputSchema` for structured chat and single-turn results. ADK v2.2.0 supports output schemas with tools through its native response mechanism. Task children use native `finish_task` with the same output schema.

A `single_turn` AgentNode emits typed `Event.Output` normally. A root `chat` coordinator is a terminal special case because ADK chat AgentNodes do not emit workflow output: duto assigns a private run-namespaced `OutputKey`, consumes the runner iterator, then reads and validates that final state value as the workflow result. A chat coordinator cannot feed downstream steps or use an in-graph finish route; its sole terminal result is projected after the run.

Duto does not parse an extra custom final-text envelope. M1 has no portable `artifacts` declaration. `max_artifact_bytes` bounds admitted context and leaves room for a later artifact contract. The optional trusted evidence directory is governed by ADR 004 and is not workflow-controlled.

## Retry, timeout, and failure

Use native ADK `workflow.NodeConfig`.

Portable retry is intentionally small:

```yaml
retry:
  max_attempts: 3
  initial_delay: 1s
  max_delay: 8s
```

Duto builds ADK `RetryConfig` with exponential backoff. The shipped adapter recognizes normalized timeout errors. Additional rate-limit and unavailable categories require stable typed errors from the provider adapter and are not inferred from strings.

Rules:

- Omitted retry means one attempt.
- Attempts include the first call.
- Invalid input/output, authorization, policy, protocol, cancellation, and domain outcomes are not retried.
- Automatic retry is rejected when the transitive step can mutate, execute a process, request/perform a remote effect, or delegate to such an agent.
- Usage, errors, time, and call debits from every attempt remain evidence.

Static step `timeout` compiles to native `NodeConfig.Timeout` and may only narrow the workflow limit. Native delegated children share the parent/workflow context deadline; v1 does not promise a generated per-child timeout because ADK's native SubAgent tool does not expose child `NodeConfig`.

V1 is fail-fast for unhandled node errors:

- any unhandled step/node error produces workflow `failed`;
- failed steps produce no validated output;
- expected optional unavailability is a successful typed outcome such as `unavailable` or `completed_with_gaps`;
- intentional `no_action`, `diagnosis_only`, or `awaiting_input` is a successful domain outcome, not failure.

All-terminal failed-step recovery is deferred rather than implemented through a hidden scheduler/wrapper.

## Limits

Trusted runtime configuration owns hard ceilings and concrete resources. Portable workflow YAML requests only bounded values:

```yaml
limits:
  timeout: 10m
  max_iterations: 20
  max_model_calls: 20
  max_tool_calls: 50
  max_concurrency: 2
  max_parallel_calls: 4
  max_artifact_bytes: 8388608

tool_limits:
  files.read: {max_calls: 12, timeout: 15s, max_result_bytes: 262144}
```

Ownership and enforcement are fixed:

- workflow root limits are run-wide budgets;
- named-agent limits are narrower per-activation budgets;
- step limits are narrower per-step budgets;
- exact-name `tool_limits` are narrower per-tool calls, deadlines, bytes, and typed family resources;
- trusted configuration is the absolute ceiling for every dimension.

Omission inherits an already-bounded value and never means unlimited. Agent and step values may only narrow. A `tool_limits` key must name a tool in that scope's exact effective set. Any explicit or unprovable widening is rejected rather than clamped.

`max_iterations` counts semantic agent-loop turns. `max_model_calls` separately counts every model attempt, including retries or implementation-level calls that do not start another turn. Applicable per-tool, step/activation, and run counters are atomically debited before an attempt; retries consume them and failures, cancellation, or timeout do not refund them. The earliest run, activation, step, per-tool, family, or caller deadline applies.

`max_concurrency` bounds simultaneous native workflow nodes through `workflow.WithMaxConcurrency`. `max_parallel_calls` bounds model-issued calls through a bounded ADK `platform.TaskRunner` and may narrow at agent/step scope. Concurrency never grants authority. The compiler calculates transitive side effects and rejects graph overlap involving mutation, process, or effect work; the ADR 002 read/write gate serializes unsafe tool calls against all other calls. Only admitted read-only work may execute concurrently.

`max_output_tokens` remains a per-model-call request bound. Reported token usage is evidence; v1 does not claim exact pre-call aggregate-token enforcement without a provider counting contract.

## Effective-plan projection

The accepted `duto-ai plan` command emits the complete normalized plan used by one-shot `run`. For the workflow, every named-agent use, and every step it includes source tool expressions, profile provenance and exact expansion, final tool names in catalog-name byte order, parent authority, capability/side-effect classes, symbolic resources, exact per-tool limits, and final run/activation/step iteration, call, timeout, artifact, and concurrency limits. It also includes trusted ceiling and catalog identifiers/digests without secret values or concrete host roots.

Every instruction includes its normalized kind, symbolic workspace/path when applicable, frozen source bytes and SHA-256 digest, source and rendered bounds, statically known template-data dependencies, and `static` or `deferred` rendering state. The plan contains admitted instruction and skill content, so callers must protect a saved plan according to that source material. A plan has schemas but no runtime workflow-input values; data-dependent rendering remains deferred. Run evidence omits raw rendered prompts.

The plan also includes the normalized direct/routed terminal result, every reachable terminal `(step, outcome)` pair, complete static coverage, deterministic route order, and the derived terminal discriminated schema.

Text and schema-versioned JSON contain the same semantics and exact values; neither substitutes an ambiguous `inherited` marker. Any prompt, tool/profile, result-coverage, or ceiling error prevents plan emission and causes zero model/tool calls. `run` compiles the same immutable effective plan internally; it does not require a preceding `plan` invocation.

## Clarification

Clarification is a terminal domain result:

```yaml
output:
  type: object
  properties:
    outcome: {type: string, enum: [ready, awaiting_input]}
    message: {type: string, max_length: 4096}
    questions:
      type: array
      max_items: 10
      items: {type: string, max_length: 1024}
  required: [outcome]
```

When `outcome: awaiting_input`, M1 returns the typed result. M2 may project it through one-shot Action outputs or summaries, and M3 may stage/publish the message through a runtime-bound conversation interface under ADR 003. Durable reply correlation and ADK workflow resume remain future-host work rather than M1 dependencies.

Native `RequestInput`/`Workflow.Resume` remains deferred for a later interactive product.

## Compilation to ADK

After all source/policy checks:

1. Resolve logical models through ADR 001.
2. Build restricted native skill sources/toolsets.
3. Build exact guarded ADK toolsets from ADR 002.
4. Construct `llmagent` values with native modes, schemas, bounded instruction providers over frozen literal/file/template sources, subagents, and a private OutputKey only for the terminal root-chat special case.
5. Build native `AgentNode` values.
6. Build small `FunctionNode` values for typed input construction and route emission.
7. Use `JoinNode` only as the native all-succeeded barrier required by the graph.
8. Apply native static-step `NodeConfig` timeout/retry and validated read-only workflow concurrency.
9. For each duto run, construct a fresh ADK runner, unique session ID, run-scoped in-memory session/artifact services, no memory service, namespaced state keys, and a fresh serialized ADR 004 evidence plugin/writer. Any synthetic single-node wrapper used for a direct root LlmAgent is ADK's native runner path, not a duto scheduler.

Private ADK state keys may connect generated nodes, but they are not workflow syntax or public result fields.

## Validation order

Validation is fixed and fail-closed:

1. Decode YAML structure and source spans.
2. Validate version, names, closed unions, and scalar bounds.
3. Validate trusted provider/model/workspace/tool records without opening resources.
4. Validate portable aliases, schemas, prompt unions/files/templates, profiles, and exact tool/workspace sets.
5. Validate named-agent modes, subagent graph, context sources, template data dependencies, and transitive authority.
6. Validate step IDs, dependencies, cycles, ADK mode placement, fail-fast fan-in, and all-read-only concurrency.
7. Validate typed bindings, route predicates, terminal-result reachability/coverage/schema, artifacts, retry, and limits.
8. Resolve trust and compile the immutable effective plan.
9. Verify evidence capacity and produce the complete `plan` output/digest.
10. Only then construct providers, tools, agents, ADK nodes, sessions, or external clients.

A failure through step 9 results in zero calls to those external/construction boundaries.

## Complete examples

### Linear workflow

```yaml
version: 1
name: linear-review
model: capable
tools: []
limits: {timeout: 5m, max_iterations: 4, max_model_calls: 4, max_tool_calls: 10, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
inputs:
  objective: {schema: {type: string, max_length: 4096}}
steps:
  - id: inspect
    needs: []
    instruction: {text: Inspect the objective.}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        findings: {type: string, max_length: 8192}
      required: [outcome, findings]
  - id: report
    needs: [inspect]
    instruction: {text: Summarize the findings.}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {findings: {type: string, max_length: 8192}}
      required: [findings]
    with: {findings: {output: {step: inspect, path: [findings]}}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 16384}
      required: [outcome, report]
result: {step: report}
```

### Parallel fan-in

```yaml
version: 1
name: parallel-review
model: light
tools: [files.read, git.read.log]
limits: {timeout: 5m, max_iterations: 6, max_model_calls: 6, max_tool_calls: 12, max_concurrency: 2, max_parallel_calls: 2, max_artifact_bytes: 0}
inputs:
  objective: {schema: {type: string, max_length: 4096}}
steps:
  - id: scan-code
    needs: []
    instruction: {text: Inspect code concerns.}
    tools: [files.read]
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        findings: {type: string, max_length: 8192}
      required: [outcome, findings]
  - id: scan-history
    needs: []
    instruction: {text: Inspect history concerns.}
    tools: [git.read.log]
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        findings: {type: string, max_length: 8192}
      required: [outcome, findings]
  - id: summarize
    needs: [scan-code, scan-history]
    wait: all_succeeded
    instruction: {text: Combine both finding sets in source order.}
    tools: []
    workspaces: []
    input:
      type: object
      properties:
        code: {type: string, max_length: 8192}
        history: {type: string, max_length: 8192}
      required: [code, history]
    with:
      code: {output: {step: scan-code, path: [findings]}}
      history: {output: {step: scan-history, path: [findings]}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 16384}
      required: [outcome, report]
result: {step: summarize}
```

### Clarification through GitHub

```yaml
version: 1
name: clarify-or-plan
model: capable
tools: []
limits: {timeout: 5m, max_iterations: 4, max_model_calls: 4, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
inputs:
  request: {schema: {type: string, max_length: 8192}}
steps:
  - id: clarify
    needs: []
    instruction: {text: Decide whether the request is actionable.}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {request: {type: string, max_length: 8192}}
      required: [request]
    with: {request: {input: request}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [ready, awaiting_input]}
        message: {type: string, max_length: 4096}
        questions:
          type: array
          max_items: 10
          items: {type: string, max_length: 1024}
      required: [outcome]
  - id: plan
    needs: [clarify]
    when:
      - step: clarify
        outcome_in: [ready]
    instruction: {text: Produce the implementation plan.}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {request: {type: string, max_length: 8192}}
      required: [request]
    with: {request: {input: request}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        plan: {type: string, max_length: 16384}
      required: [outcome, plan]
result:
  routes:
    - when: {step: clarify, outcome: awaiting_input}
      step: clarify
    - when: {step: plan, outcome: completed}
      step: plan
```

The `awaiting_input` result may become a runtime-bound `conversation.reply` request under ADR 003. It does not pause this ADK run.

### Native orchestrator delegation with snapshot context

```yaml
version: 1
name: delegated-review
model: capable
tools: [web.fetch]
limits: {timeout: 10m, max_iterations: 10, max_model_calls: 10, max_tool_calls: 20, max_concurrency: 1, max_parallel_calls: 2, max_artifact_bytes: 1048576}
inputs:
  objective: {schema: {type: string, max_length: 4096}}
agents:
  researcher:
    description: Research the declared objective.
    mode: single_turn
    model: light
    instruction: {text: Return bounded evidence.}
    tools: [web.fetch]
    workspaces: []
    skills: []
    context:
      mode: snapshot
      include:
        - input: objective
    input:
      type: object
      properties: {question: {type: string, max_length: 2048}}
      required: [question]
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        evidence: {type: string, max_length: 16384}
      required: [outcome, evidence]
  coordinator:
    description: Coordinate the review.
    mode: chat
    model: capable
    instruction: {text: "Call researcher when evidence is needed, then decide."}
    # Its child must remain within this orchestrator authority.
    tools: [web.fetch]
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher]
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        decision: {type: string, max_length: 16384}
      required: [outcome, decision]
steps:
  - id: coordinate
    needs: []
    agent: coordinator
    with: {objective: {input: objective}}
result: {step: coordinate}
```

The model sees a native `researcher` tool, not `agent.delegate`. Snapshot content is runtime-built from the declared workflow input; the model cannot choose another context mode/source. Because `coordinate` is the sole root chat step, its namespaced OutputKey value is the terminal result; it has no downstream edge or in-graph finish route.

### Diagnosis-only recovery

```yaml
version: 1
name: diagnosis-only
model: capable
tools: [files.read, git.read.diff]
limits: {timeout: 8m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 20, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 1048576}
inputs:
  failure: {schema: {type: string, max_length: 16384}}
steps:
  - id: diagnose
    needs: []
    instruction: {text: Diagnose without modifying the repository.}
    tools: [files.read, git.read.diff]
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {failure: {type: string, max_length: 16384}}
      required: [failure]
    with: {failure: {input: failure}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [diagnosis_only, no_action]}
        diagnosis: {type: string, max_length: 16384}
      required: [outcome, diagnosis]
result:
  routes:
    - when: {step: diagnose, outcome: diagnosis_only}
      step: diagnose
    - when: {step: diagnose, outcome: no_action}
      step: diagnose
```

### Handled partial data

```yaml
version: 1
name: handled-partial-data
model: light
tools: [files.read, git.read.log]
limits: {timeout: 5m, max_iterations: 6, max_model_calls: 6, max_tool_calls: 12, max_concurrency: 2, max_parallel_calls: 2, max_artifact_bytes: 0}
inputs:
  objective: {schema: {type: string, max_length: 4096}}
steps:
  - id: primary
    needs: []
    instruction: {text: Produce primary findings.}
    tools: [files.read]
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        findings: {type: string, max_length: 8192}
      required: [outcome, findings]
  - id: enrichment
    needs: []
    instruction: {text: Add history when available; otherwise return unavailable without failing.}
    tools: [git.read.log]
    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {objective: {type: string, max_length: 4096}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed, unavailable]}
        context: {type: string, max_length: 8192}
      required: [outcome]
  - id: report
    needs: [primary, enrichment]
    wait: all_succeeded
    instruction: {text: Report primary findings and state whether enrichment was unavailable.}
    tools: []
    workspaces: []
    input:
      type: object
      properties:
        findings: {type: string, max_length: 8192}
        enrichment-outcome: {type: string, enum: [completed, unavailable]}
        context: {type: string, max_length: 8192}
      required: [findings, enrichment-outcome]
    with:
      findings: {output: {step: primary, path: [findings]}}
      enrichment-outcome: {output: {step: enrichment, path: [outcome]}}
      context: {output: {step: enrichment, path: [context]}, optional: true}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed, completed_with_gaps]}
        report: {type: string, max_length: 16384}
      required: [outcome, report]
result:
  routes:
    - when: {step: report, outcome: completed}
      step: report
    - when: {step: report, outcome: completed_with_gaps}
      step: report
```

Expected missing enrichment is a successful typed `unavailable` outcome. Any unhandled node/tool error still fails fast; v1 does not route failed nodes as data.

## Exact migration of the 18 current duto-test scenarios

No current file is literally unchanged because v1 requires `version: 1`, strict fields, explicit model aliases, and typed outputs. None is obsolete.

| Scenario | Class | V1 migration |
|---|---|---|
| `agent-skills` | Mechanical | Restrict native ADK skill source; typed fan-in/output. |
| `context-files` | Intentional | Explicit bounded file context; missing files fail. |
| `file-exploration` | Mechanical | Exact file tools and one typed output object per step. |
| `full-pipeline` | Intentional | Strict typed graph with bounded M1 file/Git/GitHub/web/`shell.run` compatibility under trusted ceilings. |
| `git-history` | Mechanical | Exact Git read tools/refs and typed fan-in. |
| `iteration-limits` | Intentional | Use explicit bounded iterations alongside model/tool calls and timeout. |
| `multi-model` | Mechanical | Trusted aliases plus standard generation overrides. |
| `no-tools` | Mechanical | Omitted/empty tools means no tools; no additive defaults. |
| `output-chain` | Intentional | Typed output-object path binding replaces public output keys/state assumptions. |
| `parallel-fan-in` | Intentional | Native parallel nodes/join, typed source-order input, and bounded M1 `shell.run` compatibility. |
| `prompt-from-file` | Mechanical | Explicit bounded instruction file source. |
| `retry-transient` | Mechanical | Native NodeConfig retry over normalized transient kinds. |
| `shell-exec` | Intentional | Explicit opt-in M1 `shell.run` compatibility under trusted command/workspace/environment/time/output/call ceilings; M3 may add narrower fixed aliases. |
| `skills-injection` | Mechanical | ADK skilltoolset with restricted exact skill source. |
| `system-prompt` | Mechanical | Inline instruction and typed no-tools fan-in. |
| `template-prompt-file` | Intentional | M2 Action maps host data to declared typed inputs; M1 provides the explicit bounded template-file instruction with no environment/session lookup or custom expression language. |
| `template-variables` | Intentional | M2 owns host-input mapping; M1 templates consume only declared typed inputs with no environment/session lookup. |
| `web-fetch` | Mechanical | Exact bounded HTTPS read policy and typed output. |

Set agreement and deterministic/live gate ordering are summarized in ADR 007 and verified against the public `duto-test` scenario suite.

## Go-facing implementation guidance

Keep source-decoder structs private to `internal/config` and add explicit `yaml` tags. Presence-sensitive unions belong in decoder types using `yaml.Node`; normalized domain/effective-plan types should not expose pointer forests merely to remember syntax.

Prefer:

- one consumer-owned `ModelResolver` function;
- concrete compiler/evidence modules;
- native ADK `agent.Agent`, `tool.Toolset`, `workflow.Node`, and `session.Service` interfaces;
- `context.Context` as the first parameter and never stored;
- wrapped errors with `errors.Is`/`errors.As`;
- string constants only for serialized values and invalid zero values for internal enums;
- immutable copies of slices/maps crossing plan boundaries;
- complete consumption of `iter.Seq2` during normal execution.

Do not implement the earlier large exported source-type sketches literally.

## Deferred and rejected

- A separate Duto expression language, arbitrary template function/plugin registry, or external-state lookup.
- Implicit predecessor maps or public ADK state keys.
- Top-level schema registry/imports.
- Provider/tool/plugin code in workflow YAML.
- Recursive profiles, profile-owned limits, and per-profile call counts.
- Dynamic model-created graphs or runtime-created agents.
- Shared-history/full-fork/model-generated-summary context modes.
- Aggregate token claims without a tokenizer/counting contract.
- Resumable workflow HITL/session persistence in M1; retained durable-host decisions govern that future capability.
- Custom scheduler, retry loop, result-text decoder, skill parser, or delegation protocol.

## Consequences

### Positive

- The public schema is much smaller and maps directly to ADK v2.2.0.
- One typed output object replaces ports plus a second result envelope.
- Omission is safe without verbose empty scaffolding.
- Clarification fits GitHub workflows without persistent sessions.
- Native schema, routing, retry, timeout, skill, delegation, and event behavior is reused.

### Negative

- V1 intentionally supports only all-succeeded fan-in and no separate Duto expression language; prompts may use the bounded standard Go `text/template` contract.
- Named task/chat agents have ADK placement restrictions.
- Full transcript forks and compressed context remain deferred.
- Migration intentionally removes unbounded template capability, concrete model pass-through, additive tool defaults, and parsed-but-ignored extras.
