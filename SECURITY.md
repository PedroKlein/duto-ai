# Security policy

## Supported versions

Security fixes target the latest supported pre-1.0 release. Older pre-1.0 lines are not maintained after a replacement is published.

## Report a vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a public issue for suspected credential exposure, command execution, path traversal, token misuse, evidence disclosure, or network-access vulnerabilities.

Include:

- the affected version and operating system;
- a minimal reproduction;
- expected and observed behavior;
- potential impact;
- whether a credential or private endpoint may have been exposed.

You should receive an acknowledgement within seven days.

## M1 trust boundary

M1 is a local one-shot process. The caller owns workflow provenance, checkout isolation, credentials, filesystem permissions, and network access. Duto's strict plan and tool policy limit model-visible authority, but they do not isolate the process from its operating system.

Treat portable workflows, prompt files, skill files, repository content, fetched pages, model output, and tool output as untrusted data. Trusted configuration is a control-plane input. Do not let untrusted content choose or modify it.

M1 remains the local one-shot process foundation. M2 is the official sealed read-only GitHub Action adapter. Focused M3 mutation and staged publication are governed by [ADR 011](docs/adr/011-m3-focused-authoring-contract.md).

## M2 Action trust contract (frozen)

M2 is a one-shot adapter with a closed host surface.

- Exact supported events: `workflow_dispatch`, `schedule`, `push`, `pull_request`, `issues`, `issue_comment`.
- `workflow` and `config` must be relative regular files beneath `GITHUB_WORKSPACE`; absolute paths, traversal, symlinks, and missing files reject.
- Checkout is caller-owned with `persist-credentials: false`; the Action verifies expected `HEAD` before reading trusted control files.
- Caller workflow owns the `permissions:` ceiling. Minimum documented baseline is `contents: read`; add only `pull-requests: read`, `issues: read`, or `checks: read` when needed.
- `github.token` is scoped to exact install/runtime steps, never persisted, and never used as a capability bypass.
- Fork PR or unknown trust contexts stay transitively read-only; supported non-fork contexts still cannot exceed admitted M1 authority.

M2 installer and evidence rules are also fixed:

- install only from authenticated exact-tag release metadata, with required size and `sha256:` digest verification before extraction;
- no `latest`, branch, commit SHA, mirror, cache, or unauthenticated fallback;
- upload only the redacted Action evidence artifact (`events.jsonl`, `receipt.json`, `summary.md`, `manifest.json`) with bounded retention.
- keep the full typed result and runtime evidence runner-local; do not upload content-bearing workflow output as an Action artifact.

M2 excludes writes, SafeOutputs application, durable state, pause/resume, cross-runner recovery, and async replies.

## M3 authoring and publisher trust contract

M3 accepts only strict control evidence that normalizes to `forked_pr`, `same_repository`, `scheduled`, `local`, or `unknown`. Fork and unknown contexts cannot receive mutation authority. Context, catalog capability, trusted explicit admission, exact tool scope, workspace access, and delegated-child authority are intersected before providers, tools, processes, credentials, or publishers are constructed.

The authoring process has these hard boundaries:

- exactly one trusted writable workspace;
- atomic writes beneath admitted paths with traversal and symlink rejection;
- one fixed local Git commit containing only paths written during the activation;
- no write credential, Git credential helper, remote write adapter, push, merge, force, tag, release, or ref deletion;
- closed staged operations only: one bound reply, or one namespaced branch followed by one draft pull request.

The publisher is a separate process. It verifies the manifest and every listed file, control and policy digests, repository and subject identity, checkout and source commit, correlation, operation ordering, preconditions, permission profile, and expected bundle digest before reading `GITHUB_TOKEN` or constructing its adapter. It preflights all operations before writes and returns only `applied`, `unchanged`, `rejected`, or `conflict`.

Use separate Action jobs. The author job uses read-only permissions. A reply publisher uses `actions: read`, `contents: read`, and `issues: write`; a branch/PR publisher uses `actions: read`, `contents: write`, and `pull-requests: write`. Never combine the two write profiles.

Direct remote mode, issue/check/general-comment upserts, label reconciliation, durable state, asynchronous resume, merge, force push, tags, releases, and public plugin registries remain unsupported.

## Configuration and secrets

- Keep provider credentials, tokens, endpoints, concrete model targets, workspace roots, fixed commands, and tool ceilings in trusted configuration or its environment.
- Keep `.env` local and untracked. `.env.example` contains names only.
- Do not place secrets in workflow YAML, prompt files, skill files, model inputs, tool arguments, or diagnostic fixtures.
- Review the output path before enabling `evidence.directory`. The directory is created with restrictive permissions but the result contains the declared workflow output.
- Protect saved effective plans according to their admitted prompt and skill source content.

Trusted scalar expansion occurs after strict configuration decoding. Portable workflows do not expand environment variables.

## Tool policy

Omitted or empty tool scopes grant no tools. Selected tools must fit the trusted ceiling, parent authority, exact per-tool limits, and a family-specific trusted binding. Handlers repeat resource and size checks at the I/O boundary.

Risk by family:

- Read-only file tools remain beneath their trusted workspace. M3 `files.write` is atomic, bounded, and confined to admitted paths under the one writable workspace.
- Read-only Git tools use trusted refs and workspace policy. M3 `git.write.commit` stages only paths written in the activation and creates at most one forward local commit.
- GitHub tools are read-only in M1 and are bound to one trusted endpoint, repository, subject, and ref.
- `web.fetch` can contact HTTPS hosts admitted by the trusted domain policy and follows only bounded redirects.
- `shell.run` executes the exact absolute executable and argument list from trusted configuration with a closed environment, workspace, timeout, call count, and output limits.

`shell.run` is not a sandbox. The configured executable still has the operating-system authority of the duto process. Use a dedicated runner account and external isolation where the threat model requires them.

M1 and the root M2 Action have no file write, Git mutation, GitHub mutation, remote publication, or arbitrary-method network tool. M3 adds only the focused tools and fixed publisher described above. Token presence never grants a model capability by itself.

## Results and evidence

The evidence plugin omits private thought, raw prompts, provider targets, credentials, and raw tool arguments or results. It records bounded correlation, status, output digests, and optional reported usage. Missing usage is absent rather than reported as zero.

The optional evidence bundle is one-shot output, not a trusted audit log, durable checkpoint, or replay store. `manifest.json` is written last and binds the bundle files to the plan digest. M3 version-2 bundles additionally bind policy, control evidence, source commit, operations, and recovery files. A missing or invalid manifest means the bundle is incomplete.

The typed `result.json` and CLI result payload contain workflow output values. Define narrow output schemas and avoid returning secrets or unnecessary source content.

## Deployment guidance

- Run only workflows and trusted configurations that have passed `validate` and plan review.
- Prefer no tools. Add exact names only where needed.
- Keep trusted ceilings and tool limits as small as practical.
- Do not expose network or process tools to untrusted workflow authors.
- Use read-only workspace and repository credentials for M1.
- Run `mise run check`, `mise run integration`, and `mise run scenarios` before any approved live acceptance.
