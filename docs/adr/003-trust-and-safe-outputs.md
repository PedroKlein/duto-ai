# ADR 003: Trust and safe side effects

- **Status:** Proposed revision
- **Date:** 2026-08-25
- **Scope:** Trusted-context derivation, capability decisions, clarification delivery, staged/direct remote effects, and immutable Git/GitHub invariants

## Context

Repository content, prompts, issue text, pull-request text, fetched pages, logs, model output, and tool output are untrusted data. They cannot decide whether an agent may mutate a workspace or remote resource.

Credentials and platform permissions are outer ceilings, not proof that an operation is intended. Duto therefore derives trust from Action/CLI control evidence, intersects it with tool policy, and keeps remote publication behind a typed operation contract.

## Decision drivers

- Unknown or contradictory context gets least privilege.
- A delegated child never amplifies trust.
- Fork analysis can produce useful diagnostics without obtaining write credentials.
- Remote mutation is idempotent, bounded, and reviewable.
- Git authority cannot express force-push, protected/default-branch writes, history rewrite, or merge.
- Clarification works through the originating GitHub conversation instead of a duto session database.
- Native ADK confirmation/HITL is reused only for interactive local approval; it does not replace cross-job trust validation.

## Alternatives

### A. Distributed checks in each tool

Each handler inspects environment variables and event fields independently. This is easy to start and hard to audit.

### B. Always stage remote effects

The agent emits requests and a trusted publisher applies them. This is safest but unnecessarily rigid for explicit trusted local/operator runs.

### C. One trust plan plus one operation contract

A pure trust resolver produces capability decisions. Remote operations share one validated request shape and may be staged or explicitly admitted direct.

| Concern | A: distributed | B: staged only | C: plan + contract |
|---|---|---|---|
| Auditability | Poor | Strong | Strong |
| Local ergonomics | Inconsistent | Weak | Strong |
| Credential separation | Inconsistent | Strong | Strong |
| Implementation | Repeated | Two-job only | One policy and operation path |

## Decision

Select **C**.

Trust resolution is pure and happens before providers, tools, processes, agents, or side-effect adapters are constructed. The same typed operation is validated for staged and direct transports.

## Trusted-context derivation

Normalized contexts:

- `forked_pr`
- `same_repository`
- `scheduled`
- `local`
- `unknown`

Only Action/CLI-controlled evidence may contribute:

- stable repository/owner IDs;
- event kind and trusted source repository;
- head/base repository and revision IDs;
- actor/trigger identity supplied by the platform;
- runtime policy/admission IDs;
- exact workflow run/artifact digests;
- local operator profile selected outside workflow content.

The following never elevate trust:

- prompt or workflow prose;
- issue, discussion, PR, review, or comment body;
- repository files or Git metadata supplied by the checkout;
- fetched network content;
- model/tool output;
- an available token or writable checkout;
- user-supplied fields that merely resemble trusted event fields.

Missing required evidence, contradictions, stale admissions, or mismatched repository/revision values produce `unknown`. `unknown` is behaviorally identical to `forked_pr`.

## Capability trust matrix

Legend:

- **R:** admitted only within the already-compiled read policy.
- **S:** request may be staged; no direct remote adapter/credential is present.
- **G:** requires explicit trusted runtime admission and remains within the outer platform ceiling.
- **D:** denied.

| Capability | Forked PR | Same repository | Scheduled | Local | Unknown |
|---|---:|---:|---:|---:|---:|
| `workspace.read` | R | R | R | R | R |
| `workspace.mutate` | D | G | G | G | D |
| `git.read` | R | R | R | R | R |
| `git.mutate` | D | G | G | G | D |
| `git.publish` | S | S/G | S/G | G | S |
| `process.exec` | D | G | G | G | D |
| `network.read` | R | R | R | R | R |
| `network.mutate` | D | S/G | S/G | G | D |
| `github.read` | R | R | R | R | R |
| `github.mutate` | S | S/G | S/G | G | S |
| `agent.call` | R-only children | G within parent | G within parent | G within parent | R-only children |

Additional rules:

- A cell never grants a capability absent from ADR 002's effective tool plan.
- `G` means eligible for admission, not automatically allowed.
- Fork/unknown delegation may use only transitively read-only children.
- A privileged `pull_request_target` or `workflow_run` job is not automatically trusted and must not execute untrusted fork code.
- Fetched content remains untrusted even in scheduled/local runs.

## One typed safe-output contract

Remote effects use a closed operation set:

- reply to the runtime-bound issue, pull request, discussion, or equivalent originating conversation;
- upsert an issue/check/comment;
- reconcile approved labels;
- publish one new namespaced branch;
- create or reconcile one draft pull request.

The runtime creates the authority-bearing envelope:

```json
{
  "version": 1,
  "request_id": "runtime-issued",
  "correlation_key": "runtime-issued",
  "kind": "conversation.reply",
  "mode": "staged",
  "repository": "runtime-bound",
  "subject": "runtime-bound",
  "preconditions": {},
  "payload": {}
}
```

The model supplies only the bounded operation payload exposed by its tool schema. It cannot select:

- repository, owner, issue/PR/discussion ID, or branch base;
- staged/direct mode;
- credentials or endpoint;
- request/correlation IDs;
- protected/default branch policy;
- idempotency or precondition fields.

Each operation kind has a private concrete payload type. Do not expose `Payload any` as the public Go interface.

### Staged mode

Staged is the default for remote mutation:

1. The analysis job has no remote write adapter or write credential.
2. It writes a redacted operation request plus referenced artifact digests.
3. A fixed trusted publisher downloads and verifies the manifest.
4. The publisher re-resolves trust, permissions, subject, revision, policy digest, operation dependencies, and idempotency.
5. Only then does it open the private remote adapter.

The publisher executes fixed trusted code. It does not execute repository scripts, model output, arbitrary shell, or workflow-provided code.

### Direct mode

Direct mode is allowed only when all are true:

- the normalized context permits it;
- trusted runtime policy explicitly admits the operation;
- the expected credential slot and permission ceiling are present;
- the effective-plan digest matches;
- all operation preconditions pass.

No automatic fallback occurs between staged and direct modes.

## Clarification through GitHub channels

V1 clarification is **terminal**, not resumable ADK HITL.

An agent returns:

```json
{
  "outcome": "awaiting_input",
  "output": {
    "message": "Which package should be changed?",
    "questions": ["Provide the package path."]
  }
}
```

M1 returns that typed result locally. M2 may project it through one-shot Action outputs or summaries. M3 may turn it into a `conversation.reply` safe-output request bound to the triggering issue, pull request, discussion, or equivalent configured channel.

- The workflow/model cannot choose another conversation target.
- Fork/unknown runs may stage the reply; they do not gain direct write authority.
- If no writable publisher exists, the clarification remains in `result.json` and any M2 step summary.
- A later GitHub reply starts a fresh workflow run with a new run ID.
- Durable reply correlation or session resume belongs to the future durable-host milestone.

This supports one-shot issue-driven automation without a duto session database. The retained ADK resume, session-identity, state, and checkpoint/replay decisions govern a later durable host and are not M1 dependencies.

## Idempotency and reconciliation

- `request_id` identifies one immutable request.
- `correlation_key` identifies the logical resource across retries/reruns.
- Reapplying the same request and payload returns `unchanged`.
- Same correlation with a different payload must reconcile through the operation's explicit rules or return `conflict`.
- Unknown effect state is reconciled by reading the remote resource before retrying.
- The M3 publisher verifies the staged request and effective-plan/manifest digest, then uses remote-resource preconditions and idempotency checks within one activation.
- Durable cross-activation disposition storage and effect replay belong to the future durable-host milestone.
- Dependency order is explicit: branch publication precedes draft-PR creation.

Stable dispositions are `applied`, `unchanged`, `rejected`, and `conflict`.

## Local mutation and Git invariants

Only one workspace may be writable in an authoring run. Read-only references remain separate.

Local authoring:

- starts from an attested repository/revision;
- rejects an unexpected dirty baseline;
- permits only normalized allowed paths beneath the writable root;
- rejects traversal, symlink escape, ignored-file bypass, submodule escape, and unrelated staged content;
- stages explicit pathspecs only—never `git add -A` or `git add .`;
- creates a forward-only commit chain rooted at the attested base;
- records a recovery patch/bundle before cleanup on failure.

Publication:

- creates at most one new namespaced branch;
- never updates an existing differing branch;
- never force-pushes or uses force-with-lease;
- never changes remotes, tags, arbitrary refs, or deletes refs;
- never writes the default or protected branch;
- never publishes across repositories;
- creates only a draft PR with the new branch as head;
- never merges or enables auto-merge.

These invariants are enforced in fixed handlers and rechecked by the publisher. They are not represented as model-controlled flags.

## Permission ceilings

Minimum GitHub permissions are operation-specific:

| Operation | Outer permission ceiling |
|---|---|
| Read repository/PR/issue/discussion metadata | `contents: read` plus the relevant read permission |
| Read Actions jobs/logs/artifacts | `actions: read` |
| Reply/check/label through publisher | Only the corresponding write permission |
| Publish new branch | `contents: write` |
| Create draft PR | `pull-requests: write` plus branch publication permission |

Agent analysis jobs remain read-only by default. No v1 operation needs merge, administration, workflow-write, force, or protected-branch authority.

## ADK integration

Use native ADK facilities where they fit:

- `BeforeToolCallback` and fixed handler guards enforce the compiled plan.
- `agent.Context.RequestConfirmation` or workflow input requests may confirm an interactive local direct operation.
- ADK artifact service may store runtime artifacts.

They do not replace duto's trust resolver, staged manifest, cross-job digest verification, Git invariants, or remote idempotency.

## Deterministic verification

Required negative tests assert zero calls at the protected boundary for:

- prompt/event/repository content attempting to elevate trust;
- unknown or contradictory control evidence;
- wrong repository, subject, revision, branch, path, operation, or mode;
- force/default/protected/cross-repository/merge requests;
- unrelated staging, history rewrite, remote changes, or existing differing branch;
- broken artifact digest, duplicate request ID, stale precondition, or excessive bounds;
- privileged jobs attempting to execute fork code.

Live mutation tests never substitute for these deterministic checks.

## Migration and milestone placement

- M1 implements pure trust resolution, effective-plan decisions, and the admitted bounded compatibility tools.
- M2 maps one-shot GitHub Action inputs, permissions, results, summaries, outputs, and artifacts over the same CLI contract.
- M3 adds bounded workspace/Git mutation, one-activation recovery artifacts, staged safe outputs, and trusted publication.
- The future durable-host milestone owns cross-activation disposition storage, reconciliation/replay, and asynchronous reply correlation.
- Clarification is an M1 typed result; M2 may project it, while remote application follows the M3 operation contract.

Current duto-test scenarios remain read-only/compatibility evidence. Existing over-broad permissions must be reduced when their workflow migration lands.

## Rejected alternatives

- Trust from prompts, event prose, repository content, or credential presence.
- Direct remote mutation as the default.
- Different staged/direct payload semantics.
- Automatic staged/direct fallback.
- Arbitrary Git/shell commands with deny lists.
- Merge, force, protected/default-branch writes, or cross-repository mutation.
- A custom resumable clarification/session protocol in v1.

## Consequences

### Positive

- Security-sensitive decisions remain centralized and testable.
- Clarification fits GitHub issue/PR/discussion workflows without persistent sessions.
- ADK confirmation is reused where appropriate without confusing it with publication authority.
- Remote effects are idempotent and recoverable.

### Negative

- Staged publication requires a separate fixed publisher job.
- Terminal clarification starts a new run after the user replies.
- Some trusted same-repository operations still require explicit admission rather than working automatically.
