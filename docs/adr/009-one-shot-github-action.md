# ADR 009: First-class one-shot GitHub Action contract

- **Status:** Accepted contract; implementation in progress
- **Date:** 2026-08-24
- **Scope:** Public M2 Action explanation and closed reference surface

## Context

M1 shipped as the host-neutral local CLI and runtime. M2 adds an official GitHub Action adapter around that same one-shot runtime contract.

Before implementation work, M2 needs a closed public contract for inputs, outputs, supported events, process flags, trust policy, checkout and permission rules, installer behavior, evidence projection, and exclusions.

This ADR freezes that contract so tests and later reviews read one source of truth.

## Decision

M2 is an official **one-shot composite GitHub Action adapter** for the shipped M1 runtime. It does not redefine workflow semantics. It maps trusted GitHub host data to declared workflow inputs, invokes the same runtime behavior, and projects bounded Action-facing metadata.

### Action inputs (exactly four)

| Input | Required | Default | Contract |
|---|---:|---|---|
| `workflow` | yes | none | Relative workflow file path under `GITHUB_WORKSPACE` |
| `config` | no | `duto.yaml` | Relative trusted config file path under `GITHUB_WORKSPACE` |
| `version` | yes | none | Exact release tag in `vMAJOR.MINOR.PATCH` form |
| `evidence-retention-days` | no | `7` | Retention window for the uploaded redacted Action bundle |

No legacy or convenience inputs are part of M2.

### Action outputs (exactly seven)

| Output | Contract |
|---|---|
| `status` | Runtime execution status |
| `outcome` | Terminal domain outcome |
| `run-id` | Opaque runtime run identifier |
| `result-path` | Runner-local path to full typed runtime result |
| `evidence-path` | Runner-local path to Action evidence directory |
| `failed-step` | Failed step identifier when available; empty otherwise |
| `clarification-required` | `true` only for `awaiting_input`; `false` otherwise |

Values unavailable from typed runtime results are empty. `GITHUB_OUTPUT` is content-free metadata only.

### Supported events (exactly six)

- `workflow_dispatch`
- `schedule`
- `push`
- `pull_request`
- `issues`
- `issue_comment`

Unsupported events reject before provider construction.

### Process flags

- `duto-ai run --inputs FILE` reads one strict UTF-8 JSON object from a regular file.
- `--inputs -` is invalid; workflow `-` keeps exclusive ownership of stdin.
- M2 uses a trusted run-only `--evidence-directory` override that targets a fresh path under `RUNNER_TEMP`.

### Exit behavior

Once runtime execution creates a typed result, JSON mode writes exactly one newline-terminated result object to stdout before exit:

- `0` for success;
- `4` for failed or incomplete execution;
- `130` for cancellation.

If no typed result exists (usage, admission, or internal failure before execution result creation), stdout stays empty and diagnostics stay on stderr.

### Trust and checkout rules

- `workflow` and `config` must resolve to relative regular files beneath `GITHUB_WORKSPACE`.
- Absolute paths, traversal, symlinks, and missing files reject.
- Checkout is caller-owned and must use `persist-credentials: false`.
- Caller checks out `pull_request.base.sha` for `pull_request`; otherwise `github.sha`.
- Action verifies `HEAD` against expected revision before reading trusted control files.
- Missing, stale, or contradictory repository/revision/subject evidence rejects before provider construction.
- Recognized fork PRs and unknown trust classes are transitively read-only; supported non-fork contexts can use already-admitted M1 capabilities.

### Permissions and token rules

- Caller workflow owns the `permissions:` ceiling.
- Minimum documented baseline is `contents: read`; add only `pull-requests: read`, `issues: read`, or `checks: read` when the admitted plan needs them.
- `github.token` is scoped to exact install/runtime steps, never persisted, and only exposed when admitted GitHub-read capability requires it.
- Missing permissions fail closed at protected API boundaries. No broader-token or anonymous fallback.

### Installer rules

- No `latest`, branch, commit SHA, mirror, cache, or unauthenticated fallback.
- Release metadata for the exact `version` must resolve to exactly one expected archive for the runner platform.
- Asset metadata must include declared size and `sha256:` digest.
- Download uses the authenticated release-asset API and verifies byte count and SHA-256 before extraction.
- Extraction admits only a regular `duto-ai` file under `RUNNER_TEMP`.
- Supported platform mappings are Linux/macOS × X64/ARM64 to GoReleaser `linux|darwin` × `amd64|arm64`.

### Evidence rules

- Full typed runtime result and original M1 evidence bundle remain runner-local.
- M2 builds a separate atomic Action bundle with only:
  - `events.jsonl`
  - `receipt.json`
  - `summary.md`
  - `manifest.json`
- Uploaded artifact name is closed: `duto-m2-evidence-<run-id>-<workflow-digest-prefix>`.
- Retention defaults to seven days and is controlled only by `evidence-retention-days`.

### Explicit exclusions and boundaries

M2 excludes writes, SafeOutputs application, durable state, pause/resume, cross-runner recovery, and async replies.

- M2 does not add file/Git/GitHub mutation or publication authority.
- M2 does not introduce durable session identity, replay, or host-state recovery.
- M3 owns admitted mutation and staged safe outputs.
- Future durable-host milestones own persistence, pause/resume, cross-runner recovery, lifecycle reconciliation, and asynchronous reply correlation.

## Related ADRs

- [ADR 003: Trust and safe outputs](003-trust-and-safe-outputs.md)
- [ADR 004: Execution ledger](004-execution-ledger.md)
- [ADR 006: Workflow v1 contract](006-workflow-v1-contract.md)
- [ADR 008: Product center and delivery layers](008-product-center-and-delivery-layers.md)

## Consequences

- Tests and reviewers can assert one closed M2 contract before implementation details exist.
- Public docs can reference stable Action fields without claiming M2 is already shipped.
- Scope control remains explicit across M1, M2, M3, and future durable-host work.
