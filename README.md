# duto-ai

`duto-ai` is a CLI and runtime for bounded, typed AI workflow DAGs. It strictly decodes a trusted runtime configuration and a portable workflow, compiles an immutable effective plan, and executes that plan with [ADK Go v2](https://github.com/google/adk-go).

The local CLI remains the primary interface. M2 ships the sealed one-shot read-only GitHub Action documented by [ADR 009](docs/adr/009-one-shot-github-action.md) and [ADR 010](docs/adr/010-m2-delivery-completion.md). M3 adds bounded local file and Git authoring, staged safe outputs, a fixed publisher CLI, and separate author/publisher Actions. The focused M3 contract is frozen in [ADR 011](docs/adr/011-m3-focused-authoring-contract.md). Durable pause/resume, cross-runner recovery, and asynchronous reply correlation remain future hosting work.

## Build and inspect a workflow

Build the binary:

```bash
mise install
go build -o ./bin/duto-ai ./cmd/duto-ai
```

Create `duto.yaml`:

```yaml
version: 1
providers:
  default:
    type: custom-provider
    config: {}
models:
  light:
    provider: default
    target: example-model
```

Create `workflow.yaml`:

```yaml
version: 1
name: example
model: light
tools: []
limits:
  timeout: 1m
  max_iterations: 2
  max_model_calls: 2
  max_tool_calls: 0
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 0
steps:
  - id: report
    needs: []
    instruction: Return a typed report.
    tools: []
    workspaces: []
    input:
      type: object
      properties: {}
      required: []
    with: {}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 1024}
      required: [outcome, report]
result: {step: report}
```

Validate and inspect the effective plan:

```bash
./bin/duto-ai validate --config duto.yaml workflow.yaml
./bin/duto-ai plan --format json --config duto.yaml workflow.yaml > plan.json
```

The placeholder provider binding is enough for `validate` and `plan`, which do not construct a provider. Before `run`, replace it with a trusted configuration supported by the binary. Provider credentials, endpoints, and concrete model targets belong in `duto.yaml`, never in portable workflow YAML.

Run a workflow that declares no runtime inputs:

```bash
./bin/duto-ai run --format json --config duto.yaml workflow.yaml > result.json
```

## CLI reference

```text
duto-ai validate [--config FILE] [--control-evidence FILE] [--format text|json] WORKFLOW|-
duto-ai plan     [--config FILE] [--control-evidence FILE] [--format text|json] WORKFLOW|-
duto-ai run      [--config FILE] [--control-evidence FILE] [--format text|json] [--inputs FILE] [--evidence-directory DIR] WORKFLOW|-
duto-ai publish  --config FILE --control-evidence FILE --bundle DIR --expected-bundle-sha256 HEX --permission-profile reply|branch-pr --receipt FILE [--format text|json]
duto-ai version
```

`--config` defaults to `duto.yaml`. `--format` defaults to `text`. A workflow path of `-` reads one YAML document from stdin.

Each operation writes exactly one payload followed by a newline to stdout. Diagnostics go to stderr.

- `validate` emits `valid` or `{"version":1,"valid":true}`.
- `plan` emits the complete effective plan as pretty or compact JSON. The plan includes frozen instruction and skill content, so protect it according to the source material.
- `run` emits a pretty or compact typed result object.

Exit codes are stable:

| Code | Meaning |
|---:|---|
| `0` | Success |
| `1` | Unexpected internal error |
| `2` | Command usage error |
| `3` | Configuration, workflow, or admission error |
| `4` | Execution failure or incomplete execution |
| `130` | Cancellation |

### Runtime inputs

`run` accepts `--inputs FILE` for one strict UTF-8 JSON object. `--inputs -` is invalid because stdin remains reserved for `WORKFLOW=-`. When a workflow declares top-level `inputs`, `--inputs` is required and is validated before provider construction.

`run` also accepts `--evidence-directory DIR` as a trusted run-only override for the runtime evidence bundle path. `--control-evidence` accepts one bounded regular non-symlink JSON file; stdin is forbidden. M3 mutation is denied unless its normalized context, trusted `m3` admission, exact workspace, and selected capabilities all agree.

`publish` verifies the complete staged bundle, current control evidence, policy digest, repository identity, source commit, permission profile, operation ordering, and expected manifest digest before reading `GITHUB_TOKEN` or constructing a remote adapter. Its dispositions are `applied`, `unchanged`, `rejected`, and `conflict`.

## Use the one-shot GitHub Action (M2)

M2 wraps the same admitted one-shot runtime path used by the local CLI. Caller workflows own checkout and permission ceilings.

### Minimal caller workflow (pinned actions)

```yaml
name: duto

on:
  workflow_dispatch:
  schedule:
    - cron: "0 4 * * *"
  push:
  pull_request:
  issues:
  issue_comment:

jobs:
  run-duto:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      # Add only when the admitted workflow uses GitHub read tools:
      # pull-requests: read
      # issues: read
      # checks: read
    steps:
      - if: github.event_name == 'pull_request'
        uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332
        with:
          ref: ${{ github.event.pull_request.base.sha }}
          persist-credentials: false

      - if: github.event_name != 'pull_request'
        uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332
        with:
          ref: ${{ github.sha }}
          persist-credentials: false

      - name: Run duto-ai
        uses: PedroKlein/duto-ai@462e48601658765e96448a147fbcd029f034e329
        with:
          workflow: .github/ai-workflows/scenarios/template-variables.yaml
          config: .github/ai-workflows/config-m2.yaml
          version: v0.3.1
          evidence-retention-days: "7"
```

Pin both `actions/checkout` and `PedroKlein/duto-ai` to full 40-character commit SHAs. Update those pins through your normal dependency-review process.

### Action reference

- **Exact inputs:** `workflow`, `config` (default `duto.yaml`), `version` (`vMAJOR.MINOR.PATCH`), `evidence-retention-days` (default `7`)
- **Exact outputs:** `status`, `outcome`, `run-id`, `result-path`, `evidence-path`, `failed-step`, `clarification-required`
- **Supported events:** `workflow_dispatch`, `schedule`, `push`, `pull_request`, `issues`, `issue_comment`
- **Checkout contract:** caller-owned checkout with `persist-credentials: false`; use `pull_request.base.sha` for `pull_request`, otherwise `github.sha`
- **Permissions contract:** baseline `contents: read`; add only `pull-requests: read`, `issues: read`, and `checks: read` when needed by admitted GitHub read tools
- **Failure handling:** if runtime emits a typed result, JSON mode writes exactly one newline-terminated payload before exit `0`, `4`, or `130`; pre-result usage/admission/internal failures keep stdout empty
- **Evidence and retention:** full typed result and runtime evidence remain runner-local; uploaded artifact is the redacted Action bundle with configured retention days (subject to repository policy)
- **Process boundary:** `shell.run` and runner execution are not a sandbox
- **Exclusions:** no writes, no SafeOutputs application, no durable state, no pause/resume, no cross-runner recovery, and no async replies

## Use focused authoring and staged publication (M3)

M3 uses separate jobs and separate composite Actions. The author job has no remote write credential. It may write beneath one admitted workspace, create one deterministic local commit, and stage either one bound conversation reply or one namespaced branch plus one draft pull request. The publisher job downloads that exact bundle and applies only the fixed staged operation set.

```yaml
jobs:
  author:
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332
        with:
          persist-credentials: false
      - id: author
        uses: PedroKlein/duto-ai/author@01ec76d320e9d2f4a7a3f38646f3a2daebf25117
        with:
          workflow: .github/ai-workflows/author.yaml
          config: duto.yaml
          version: vMAJOR.MINOR.PATCH
          correlation-key: request-123

  publish:
    needs: author
    permissions:
      actions: read
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332
        with:
          persist-credentials: false
      - uses: PedroKlein/duto-ai/publish@01ec76d320e9d2f4a7a3f38646f3a2daebf25117
        with:
          config: duto.yaml
          version: vMAJOR.MINOR.PATCH
          artifact-id: ${{ needs.author.outputs.artifact-id }}
          artifact-digest: ${{ needs.author.outputs.artifact-digest }}
          bundle-sha256: ${{ needs.author.outputs.bundle-sha256 }}
          permission-profile: branch-pr
```

Use `issues: write` with `contents: read` for the `reply` profile. Do not combine reply and branch/PR permissions in one publisher job. Direct remote mode, issue/check/general-comment upserts, label reconciliation, merge, force push, tag, release, durable sessions, and public plugin registries are not supported.

## Trusted configuration reference

The trusted configuration is a strict `version: 1` YAML document. Its root fields are:

| Field | Required | Value |
|---|---:|---|
| `version` | yes | Integer `1` |
| `providers` | yes | Map of provider alias to `{type, config}` |
| `models` | yes | Map of model alias to `{provider, target}` |
| `workspaces` | no | Map of symbolic name to `{root, access}`; M1/M2 accept `read`, while admitted M3 allows exactly one `write` workspace |
| `tool_profiles` | no | Map of profile name to a flat selector list |
| `tools` | no | Trusted tool ceiling as a selector list; omission means an empty ceiling |
| `tool_limits` | no | Exact tool-name map of hard limits |
| `tool_config` | no | Closed trusted bindings for selected tool families |
| `m3` | no | Closed admission, authoring bounds, and staged-publication policy from ADR 011 |
| `evidence` | no | `{directory}` for an optional one-shot evidence bundle; required by M3 |

A `tool_limits` entry accepts `max_calls`, `timeout`, `max_request_bytes`, and `max_result_bytes`. Every selected tool must have a positive trusted limit. Portable workflow and child limits may narrow this record but cannot widen it.

`tool_config` has these closed family records:

- `files`: `workspace`
- `git`: `workspace`, `refs`, `allow_working_tree`, `max_log_count`
- `github`: `base_url`, optional `token`, `owner`, `repository`, `subject`, `ref`, `max_pages`, `max_results`
- `web`: `allowed_domains`, `max_redirects`
- `shell`: absolute `executable`, fixed `args`, `workspace`, `environment`, `max_stdout_bytes`, `max_stderr_bytes`

Trusted provider, workspace, evidence, GitHub, web, and shell scalar values may expand environment variables after structural decoding. Expanded secret values are not copied into plan output or diagnostics. Portable workflows never expand environment variables.

## Portable workflow reference

A workflow is a strict `version: 1` YAML document. Required root fields are `version`, `name`, `model`, `limits`, `steps`, and `result`. Optional root fields are `description`, `inputs`, `model_config`, `tool_profiles`, `tools`, `tool_limits`, `skills`, and `agents`.

Unknown fields, duplicate keys, aliases, anchors, merge keys, explicit nulls, unsupported tags, invalid UTF-8, scalar coercion, numeric overflow, and multiple documents are rejected. Diagnostics include the file, line, column, field path, and stable error code.

### Types and graph

Supported schema types are `object`, `array`, `string`, `integer`, `number`, and `boolean`. Objects are closed. Arrays require `items` and `max_items`; unconstrained strings require `max_length`. Every step or agent output is an object with a required finite `outcome` enum.

An inline step accepts:

- required `id`, `instruction`, `input`, `with`, and `output`;
- optional `needs`, `wait`, `when`, `model`, `model_config`, `tools`, `tool_limits`, `skills`, `workspaces`, `retry`, and `limits`.

A named-agent step accepts `id`, optional `needs`, `wait`, and `when`, plus required `agent` and `with`. It cannot override the selected agent.

`needs` defines a static acyclic graph. Fan-in uses `wait: all_succeeded` and builds inputs in source order. A `with` binding is exactly one of a workflow input, an ancestor output property path, or a scalar literal. There is no expression language, implicit predecessor map, or public ADK state key.

`result` is either `{step: terminal-step}` or an exhaustive list of routes keyed by a terminal step and one of its declared outcomes. `awaiting_input` is an ordinary successful domain outcome; it does not pause or resume the run.

### Limits and retry

The workflow `limits` record contains:

- `timeout`
- `max_iterations`
- `max_model_calls`
- `max_tool_calls`
- `max_concurrency`
- `max_parallel_calls`
- `max_artifact_bytes`

All required workflow values must be finite; model, tool, step, and agent scopes can only narrow inherited ceilings. Retry uses `max_attempts`, `initial_delay`, and `max_delay`. Process-capable steps cannot use automatic retry, and unsafe work cannot overlap another graph branch.

## Tools and authority

M1 has this catalog:

- Files: `files.read`, `files.find`, `files.grep`
- Git: `git.read.log`, `git.read.blame`, `git.read.show`, `git.read.diff`
- GitHub: `github.read.issue`, `github.read.pr`, `github.read.diff`, `github.read.changed-files`, `github.read.comments`, `github.read.reviews`, `github.read.checks`, `github.read.search-issues`
- Network: `web.fetch`
- Process: `shell.run`

Portable selectors may use an exact name or one of the terminal namespace wildcards `files.*`, `git.read.*`, and `github.read.*`. Broader or global wildcards reject.

A tool scope is either an array or a closed expression with `from`, `add_profiles`, `add`, `remove_profiles`, and `remove`. Profiles are flat selector lists. Trusted and workflow profile names must not collide. Expansion is deterministic and final names use catalog byte order.

Omitted tools, `tools: []`, and an empty expression expose no direct tools. Inheritance requires `from: parent`. The trusted configuration is the outer ceiling, workflow tools are the parent for top-level steps and agents, and each child must be a subset of its parent. Registration never grants authority.

Selected families also require their trusted `tool_config` binding. File, Git, GitHub, network, and process handlers repeat resource and byte checks at the I/O boundary. `shell.run` takes no model-selected command: it executes the exact absolute executable and arguments from trusted configuration with a closed environment, workspace, deadline, call count, and output bounds. It is not a sandbox.

Focused M3 additionally registers `files.write`, `git.write.commit`, `safe-output.conversation-reply`, `safe-output.branch`, and `safe-output.draft-pr`. These names appear only when trusted M3 policy is present. `files.write` is atomic and confined to the one writable workspace. `git.write.commit` stages only paths written during the activation and creates at most one forward commit. Safe-output tools write closed staged requests; they never open a remote adapter.

M3 still has no arbitrary Git command, arbitrary-method web request, model-visible GitHub write client, tool plugin registry, portable provider registry, direct remote mode, merge, force push, tag, or ref deletion.

## Instructions, templates, and skills

`instruction` accepts exactly one of:

- a scalar or `{text: string}` literal;
- `{file: {workspace, path, max_bytes}}`;
- `{template: {text, max_output_bytes}}`;
- `{template: {file: {workspace, path, max_bytes}, max_output_bytes}}`.

Files must be regular UTF-8 files beneath an admitted read workspace. Admission freezes file and template bytes into the effective plan. Traversal, symlink escape, invalid templates, unavailable data, and source or rendered-size overflow fail closed.

Templates use bounded Go `text/template` with a fixed data object: `.Workflow`, `.Step`, `.Predecessors`, and `.Runtime`. They cannot read environment variables, secrets, host events, arbitrary files, clocks, or session state.

Top-level `skills` maps names to `{workspace, path}`. An agent or inline step selects exact names. Each skill must have a matching `SKILL.md`; only bounded regular UTF-8 files under `references`, `assets`, and `scripts` are exposed through ADK's native skill toolset. Skill metadata cannot widen tool authority.

## Named agents and subagents

Named agents use `single_turn`, `task`, or `chat` mode with fixed model, instruction, schemas, tools, workspaces, skills, context, limits, and declared `subagents`. Context is either `fresh` or a bounded `snapshot` of declared workflow inputs and files.

The current native ADK integration has an important placement limit: a subagent tree is executable only from a `chat` named agent used by the workflow's sole root and terminal step. A `task` agent can be a declared child but not a static workflow step. A `single_turn` named agent can be a static step only when it has no children. Snapshot references to ancestor outputs are decoded but rejected by admission for the root-chat path because no ancestor exists there.

Each child remains inside its parent's model, tool, workspace, and limit envelope. The model sees one native tool per declared child and cannot choose a different child configuration. There is no aggregate delegation tool, nested runner, persistent child conversation, or model-created graph.

## Results and evidence

A successful `run` result contains `version`, an opaque one-shot `run_id`, `workflow`, execution `status`, domain `outcome`, timestamps, ordered step results, terminal `output`, optional reported usage, and normalized error kinds. Text output is pretty JSON; JSON output is compact JSON. Missing usage stays absent.

Every invocation uses fresh in-memory ADK session and artifact services. The runner consumes the full event stream. Raw model reasoning, prompts, provider targets, credentials, and raw tool arguments or results are not written to the evidence event stream.

When `evidence.directory` is non-empty, `run` atomically creates a new directory containing:

```text
events.jsonl
result.json
summary.md
manifest.json
```

The manifest is written last and includes the plan digest plus file sizes and SHA-256 digests. The target directory must not already exist. M3 writes a version-2 manifest with policy/control/source bindings, closed operation envelopes, and recovery files when needed. This bundle records one execution; it is not a durable session, checkpoint, or replay store.

## Delivery boundaries

| Milestone | Scope |
|---|---|
| M1, shipped | Local `validate`, `plan`, and one-shot `run`; strict v1 documents; bounded typed DAGs; read/process tools; native finite subagents; typed results and evidence |
| M2, shipped | Official one-shot GitHub Action mapping trusted host inputs to the same CLI contract, then projecting summaries, outputs, and artifacts |
| M3, shipped | One admitted writable workspace, atomic file writes, one local commit, staged reply or branch/draft-PR operations, fixed publisher CLI, and separate Actions |
| Future durable hosting | Persistent pause/resume, encrypted host state, cross-runner recovery, lifecycle reconciliation, and asynchronous replies |

## Development

```bash
mise install
mise run check
mise run integration
mise run scenarios
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for contributor commands, [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for runtime design, and [docs/adr/006-workflow-v1-contract.md](docs/adr/006-workflow-v1-contract.md) for the full workflow contract.

## License

Licensed under the [Apache License 2.0](LICENSE).
