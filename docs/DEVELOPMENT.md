# Development

This guide covers deterministic local development for the CLI-first M1 codebase.

## Prerequisites

- [mise](https://mise.jdx.dev/)
- Git

Install the pinned Go toolchain and linter:

```bash
mise install
```

The project uses Go 1.26.5 and ADK Go v2.2.0. The default test and integration suites use fakes and require no provider credentials or network access.

## Deterministic commands

| Command | Purpose |
|---|---|
| `mise run build` | Build all packages |
| `mise run vet` | Run `go vet` |
| `mise run lint` | Run golangci-lint |
| `mise run test` | Run unit tests with the race detector |
| `mise run check` | Run build, vet, lint, and race-enabled unit tests |
| `mise run integration` | Run integration-tagged workflows with fake models and boundaries |
| `mise run scenarios` | Check the exact scenario set and forbidden executable markers |
| `mise run coverage` | Generate `coverage.out` and `coverage.html` |
| `mise run tidy` | Update module metadata with `go mod tidy` |

Before opening a pull request, run:

```bash
mise run check
mise run integration
mise run scenarios
go mod tidy -diff
git diff --check
```

The first three commands are deterministic gates. Run any approved live model or hosted acceptance only after they pass. A live pass does not replace them.

## Verify the process contract

Build a temporary binary and use the provider-neutral fixtures:

```bash
go build -o /tmp/duto-ai ./cmd/duto-ai
/tmp/duto-ai validate --format json --config testdata/config.yaml testdata/workflow.yaml
/tmp/duto-ai plan --format json --config testdata/config.yaml testdata/workflow.yaml
```

The fixture provider is a placeholder suitable for admission tests. Do not use it as a live provider configuration. For `run`, use a trusted provider configuration and pass workflow inputs with `--inputs FILE` when the workflow declares top-level `inputs`. `FILE` must be a strict UTF-8 JSON object from a regular file.

## Test boundaries

- Config tests cover strict YAML structure and source diagnostics.
- Plan tests cover aliases, typed schemas and bindings, result routes, tools, profiles, ceilings, limits, prompts, skills, and agent authority before construction.
- Compiler tests pin native ADK schema, workflow, retry, timeout, skill, and subagent behavior.
- Tool-family tests use temporary directories, temporary Git repositories, HTTP test servers, and fixed executables.
- Runtime tests consume complete ADK event streams and verify typed results and redacted evidence.
- Command tests run both the Cobra command tree and a built binary, checking stdout, stderr, and exit codes.

Use handwritten fakes at model, HTTP, filesystem, process, and host boundaries. Do not export internal symbols solely for tests.

## Documentation checks

When public flags, schema fields, tools, provider seams, result fields, or host adapters change:

1. Update `README.md` and the relevant reference or architecture document.
2. Update an ADR when the accepted contract or milestone boundary changes.
3. Decode complete YAML examples with the strict loader through the built CLI.
4. Run the top-level documentation validators: `go test -race ./internal/actiontest -run '^TestDocs_' -count=1 -v`.
5. Check relative Markdown links.
6. Search tracked and untracked non-ignored files for credentials, private endpoints, workstation paths, internal model identifiers, and private planning metadata.
7. Search public docs for removed command flags, fields, and tool names.

Keep public examples and fixtures provider-neutral. Never add real credentials or private endpoints to `.env.example`, tests, docs, logs, or evidence.

## Repository structure

See [ARCHITECTURE.md](ARCHITECTURE.md) for package responsibilities and execution flow. The accepted source contracts are recorded in [ADR 002](adr/002-tool-catalog-configuration.md), [ADR 005](adr/005-agent-delegation.md), [ADR 006](adr/006-workflow-v1-contract.md), and [ADR 008](adr/008-product-center-and-delivery-layers.md).

## Pull requests

- Use a focused branch with one of the prefixes documented in [CONTRIBUTING.md](../CONTRIBUTING.md).
- Add behavior tests for code changes and regression tests for fixes.
- Keep Action implementation, mutation/publication, durable hosting, and release work out of an M1 core change unless the issue explicitly targets that later milestone.
- Do not commit generated coverage files, credentials, evidence bundles, or local environment files.
