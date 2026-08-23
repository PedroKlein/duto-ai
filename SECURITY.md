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

M1 does not provide a GitHub Action, mutation tools, remote publication, durable sessions, or cross-runner recovery. M2 will own Action event and permission mapping. M3 will own mutation and publication policy.

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

- File tools can read only beneath their trusted workspace and reject traversal and symlink escape.
- Git tools use fixed read-only operations, trusted refs, and a trusted workspace policy.
- GitHub tools are read-only in M1 and are bound to one trusted endpoint, repository, subject, and ref.
- `web.fetch` can contact HTTPS hosts admitted by the trusted domain policy and follows only bounded redirects.
- `shell.run` executes the exact absolute executable and argument list from trusted configuration with a closed environment, workspace, timeout, call count, and output limits.

`shell.run` is not a sandbox. The configured executable still has the operating-system authority of the duto process. Use a dedicated runner account and external isolation where the threat model requires them.

M1 has no file write, Git mutation, GitHub mutation, remote publication, or arbitrary-method network tool. Token presence never grants a model capability by itself.

## Results and evidence

The evidence plugin omits private thought, raw prompts, provider targets, credentials, and raw tool arguments or results. It records bounded correlation, status, output digests, and optional reported usage. Missing usage is absent rather than reported as zero.

The optional evidence bundle is one-shot output, not a trusted audit log, durable checkpoint, or replay store. `manifest.json` is written last and binds the bundle files to the plan digest. A missing or invalid manifest means the bundle is incomplete.

The typed `result.json` and CLI result payload contain workflow output values. Define narrow output schemas and avoid returning secrets or unnecessary source content.

## Deployment guidance

- Run only workflows and trusted configurations that have passed `validate` and plan review.
- Prefer no tools. Add exact names only where needed.
- Keep trusted ceilings and tool limits as small as practical.
- Do not expose network or process tools to untrusted workflow authors.
- Use read-only workspace and repository credentials for M1.
- Run `mise run check`, `mise run integration`, and `mise run scenarios` before any approved live acceptance.
