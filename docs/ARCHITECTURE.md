# Architecture

This document explains the shipped CLI-first M1 runtime. [ADR 008](adr/008-product-center-and-delivery-layers.md) defines the product boundary and later delivery layers.

## Product boundary

Duto validates, inspects, and executes a finite typed workflow. Workflow generation, open-ended planning, backlog management, and dynamic graph expansion belong to callers.

The core is host-neutral. M1 is a local process interface. M2 is the official one-shot GitHub Action adapter in this repository, with contract details frozen in [ADR 009](adr/009-one-shot-github-action.md). Mutation and publication are M3. Durable sessions and cross-runner recovery remain future work.

```text
caller
  -> duto-ai validate | plan | run
     -> strict trusted-config decode
     -> strict portable-workflow decode
     -> pure effective-plan compilation
     -> validate: one validity payload
     -> plan: one normalized plan payload
     -> run: late provider and tool construction
        -> native ADK graph and runner
        -> typed terminal result
        -> optional redacted one-shot evidence bundle
```

Admission completes before provider, model, tool handler, client, process, agent, or ADK node construction. This makes invalid configuration a zero-call failure.

## Command boundary

`cmd/duto-ai` is the composition root. Cobra exposes `validate`, `plan`, `run`, and `version`.

All three operation commands accept one workflow file or `-` for stdin, `--config FILE`, and `--format text|json`. They write one payload to stdout and diagnostics to stderr. The command maps usage, admission, execution, cancellation, and internal failures to distinct exit codes.

The composition root also owns the bundled provider adapter and concrete run-scoped tool construction. Core packages receive narrow model and toolset resolver functions; they do not read provider credentials or construct host clients.

The CLI accepts workflow input values through `run --inputs FILE`, which must be a strict UTF-8 JSON object read from a regular file. `--inputs -` is rejected so stdin remains reserved for `WORKFLOW=-`. If a workflow declares root `inputs`, `--inputs` is required and enforced before provider construction.

`run` also accepts `--evidence-directory DIR` as a trusted run-only override for the runtime evidence bundle path.

## Strict source documents

`internal/config` decodes both YAML documents through `yaml.Node`. It rejects unknown fields at every level, duplicate keys, aliases, anchors, merges, explicit null, unsupported tags, invalid UTF-8, scalar coercion, overflow, and multiple documents.

The trusted document owns provider bindings, concrete model targets, workspace roots, the tool ceiling, tool-family bindings, hard tool limits, and the optional evidence directory. Environment expansion occurs only in selected trusted scalar fields after structural decoding.

The portable document owns logical model aliases, bounded instructions and skills, static typed graph structure, exact tool and workspace subsets, retry, limits, and terminal result selection. Portable text is never environment-expanded.

Source diagnostics contain file, line, column, field path, and a stable code. Secret values are not part of those diagnostics.

## Effective plan

`internal/plan` compiles the two decoded documents without constructing execution resources. The result is copied through deterministic JSON and exposed through immutable snapshots.

The plan contains:

- logical model aliases and provider-neutral generation settings;
- closed workflow, step, and agent schemas;
- typed bindings and source-ordered dependencies;
- normalized instructions, frozen file or skill bytes, and SHA-256 digests;
- exact tool names, profile expansion, capabilities, side-effect classes, parent authority, and narrowed limits;
- finite workflow, agent, step, retry, concurrency, and artifact-byte limits;
- direct or routed terminal-result coverage;
- a digest over the normalized plan.

The plan omits provider configuration values, credentials, concrete model targets, and concrete workspace roots. It does contain admitted instruction and skill source content, so a saved plan must be protected like that source material.

## Graph and typed data

`internal/compiler` converts an effective plan into native ADK agents and workflow nodes.

Each ordinary step uses an ADK `llmagent` inside an `AgentNode`. Small generated function nodes construct typed input objects and terminal results. `needs` produces static edges, and multiple dependencies use native all-succeeded fan-in. `workflow.WithMaxConcurrency` applies the admitted graph concurrency bound.

A step input is assembled only from declared workflow inputs, scalar literals, and typed ancestor output paths. Runtime values are validated against compiled schemas without coercion. Step output is a closed object with a required finite `outcome` enum. The direct result or exhaustive route set selects exactly one terminal output.

Unhandled node errors fail fast. Expected absence is data, represented by a successful domain outcome such as `unavailable` or `awaiting_input`. A successful `awaiting_input` result ends the invocation; M1 does not pause it.

## Prompts and skills

`internal/prompt` admits literal, file, template, and template-file instructions. File access uses an admitted symbolic read workspace. Regular-file, traversal, symlink, UTF-8, source-size, template-operation, and rendered-size checks happen before a model receives text.

Admission freezes prompt and skill bytes. Execution renders templates against the closed `.Workflow`, `.Step`, `.Predecessors`, and `.Runtime` object. No environment, host event, credential, clock, memory, or arbitrary file lookup is available to a template.

Selected skills are frozen from an admitted workspace and exposed through ADK's `skilltoolset` using a restricted source. Only the selected `SKILL.md` and bounded resources under `references`, `assets`, and `scripts` are visible. Skill metadata is instruction, not authority.

## Tools and authority

`internal/tool` contains a fixed M1 catalog. Catalog registration describes availability; plan compilation grants authority.

The policy compiler resolves flat profiles and exact selectors. It permits exact names plus `files.*`, `git.read.*`, and `github.read.*`. The trusted ceiling bounds the workflow. Named agents and steps are exact subsets of their parent scope, with explicit `from: parent` inheritance only.

Each selected tool has one trusted hard limit and may have narrower workflow, agent, or step limits. A common ADK guard atomically debits attempts and applies the earliest deadline. Family handlers repeat resource, request, and result checks at the I/O boundary.

Family behavior is deliberately narrow:

- file tools read, find, or grep beneath one trusted read workspace;
- Git tools run fixed read-only operations against trusted refs and workspace policy;
- GitHub tools read runtime-bound repository or review data from one trusted endpoint and subject;
- `web.fetch` uses an HTTPS domain allowlist and bounded redirects;
- `shell.run` executes one exact trusted executable and fixed argument list with a closed environment and bounded output.

The process tool is not a sandbox. Process-capable work cannot be retried automatically or overlap another graph branch. M1 has no mutation or publication tool.

## Native subagents

Named agents compile to ADK `llmagent` values with `single_turn`, `task`, or `chat` mode. A declared child becomes a native model-callable tool. ADK owns dispatch, task completion, function responses, cancellation, and event lineage.

The current compiler admits a child tree only beneath a named `chat` agent used as the workflow's sole root and terminal step. This is an ADK root-placement constraint in the shipped integration:

- a root chat step has no predecessors and no downstream step;
- a `task` agent is a child, never a static graph step;
- a static `single_turn` named agent cannot declare children;
- snapshot context for this path may include declared workflow inputs and admitted files, but not ancestor outputs.

Child tool, workspace, model, depth, call, byte, and parallel limits are compiled as subsets of parent authority. Read-only single-turn child calls may use the bounded ADK task runner. Task children and unsafe work are sequential. Runs do not share child transcripts, state, artifacts, or memory.

## Runtime and evidence

`internal/runtime` validates input values, compiles the ADK graph, creates a fresh random run ID, and constructs a fresh ADK runner with in-memory session and artifact services. No memory service or durable store is installed. The runtime consumes the complete event iterator.

A concrete runner plugin projects bounded evidence. It keeps safe invocation, event, node, function-call correlation, output digest, status, and optional usage facts. It omits private thought, raw content, raw prompts, raw tool arguments and results, credentials, provider targets, and concrete model targets.

The result has one execution status and one independent domain outcome. It contains ordered step summaries, typed outputs, optional reported usage, normalized error kinds, and timestamps. Missing usage remains absent.

If trusted configuration sets `evidence.directory`, the runtime creates a new bundle atomically:

```text
events.jsonl
result.json
summary.md
manifest.json
```

`manifest.json` is written last and binds file sizes and SHA-256 digests to the plan and run. The directory cannot already exist. Evidence is one-shot diagnostic output, not a session checkpoint or replay protocol.

## Package responsibilities

| Package | Responsibility |
|---|---|
| `cmd/duto-ai` | CLI contract, exit mapping, provider and tool wiring |
| `internal/config` | Strict trusted and portable YAML decoding |
| `internal/plan` | Pure normalization, policy admission, graph checks, deterministic plan |
| `internal/prompt` | Bounded prompt/template admission and restricted skills |
| `internal/compiler` | Effective plan to native ADK agents and workflow nodes |
| `internal/runtime` | Fresh-run execution, event folding, typed result, evidence bundle |
| `internal/tool` | Fixed catalog, selectors, guards, and ADK toolset adapter |
| `internal/tool/files` | Bounded workspace reads |
| `internal/tool/git` | Bounded read-only Git operations |
| `internal/tool/github` | Bounded GitHub reads |
| `internal/tool/web` | Bounded HTTPS fetch |
| `internal/tool/shell` | Exact trusted process execution |
| `internal/testing/mockllm` | Deterministic ADK model fake |

## Verification boundaries

`mise run check` builds, vets, lints, and runs race-enabled unit tests. `mise run integration` executes complete graphs with deterministic fake models and boundary fakes. `mise run scenarios` checks the exact scenario ownership set and rejects executable legacy markers.

Live model or hosted acceptance can supplement those gates only after they pass. It does not replace deterministic validation.

## Later delivery layers

M2 packages the same one-shot CLI and runtime contract as the official GitHub Action adapter (`action/install.sh`, `action/prepare.sh`, `action/run.sh`, and `action/project.sh`). It owns event-to-input mapping, permissions, summaries, outputs, and artifact upload.

The frozen M2 reference surface is:

- four inputs: `workflow`, `config`, `version`, `evidence-retention-days`;
- seven outputs: `status`, `outcome`, `run-id`, `result-path`, `evidence-path`, `failed-step`, `clarification-required`;
- six events: `workflow_dispatch`, `schedule`, `push`, `pull_request`, `issues`, `issue_comment`.

M2 also fixes path confinement, caller-owned checkout and permission ceiling, authenticated checksum-verified installer, and redacted Action evidence projection as documented in ADR 009.

M2 excludes writes, SafeOutputs application, durable state, pause/resume, cross-runner recovery, and async replies.

M3 adds admitted workspace and Git mutation, staged safe-output requests, and a fixed trusted publisher. It does not retroactively grant write authority to M1 tools.

A future durable-host milestone may add persistence, pause/resume, encrypted host state, cross-runner recovery, effect replay, and asynchronous reply correlation. None of those facilities is required or implied by the current one-shot evidence bundle.
