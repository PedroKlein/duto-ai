# Development

## Prerequisites

- [mise](https://mise.jdx.dev/)
- Git
- Credentials for the bundled provider only when running smoke tests

## Setup

```bash
mise install
mise run check
```

`mise` installs the Go version declared by the project and the configured linter. Local `.env` files are loaded for smoke-test credentials and are ignored by Git.

## Commands

| Command | Purpose |
|---|---|
| `mise run build` | Build all packages |
| `mise run vet` | Run `go vet` |
| `mise run lint` | Run golangci-lint |
| `mise run test` | Run unit tests with the race detector |
| `mise run check` | Run build, vet, lint, and unit tests |
| `mise run integration` | Run full workflows with a mock model |
| `mise run smoke` | Run a live model against a fake GitHub API |
| `mise run coverage` | Generate `coverage.out` and `coverage.html` |
| `mise run tidy` | Run `go mod tidy` |

Run deterministic checks before smoke tests:

```bash
mise run check
mise run integration
mise run smoke
```

## Smoke-test credentials

Smoke tests use the adapter-specific environment variables listed in `.env.example`. Keep their values in `.env`; never commit them.

The GitHub side of the smoke suite is an `httptest` server. `GITHUB_TOKEN` may be a placeholder because no request reaches GitHub.

## Project structure

See [ARCHITECTURE.md](ARCHITECTURE.md) for package responsibilities and runtime flow.

## Pull requests

- Create a focused branch using the prefixes in `AGENTS.md`.
- Add behavior tests for code changes.
- Run `mise run check` and `mise run integration`.
- Update public documentation when changing CLI flags, workflow fields, Action inputs/outputs, providers, or tools.
- Keep examples and fixtures free of credentials, private endpoints, customer names, and internal model identifiers.
