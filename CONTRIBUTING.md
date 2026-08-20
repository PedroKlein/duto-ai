# Contributing

## Development setup

Install the toolchain and run the deterministic gates:

```bash
mise install
mise run check
mise run integration
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for individual commands and smoke-test setup.

## Submitting changes

1. Fork the repository.
2. Create a focused branch using the prefixes in `AGENTS.md`.
3. Add or update behavior tests.
4. Run `mise run check` and `mise run integration`.
5. Open a pull request using a rebase merge.

## Guidelines

- Follow the style enforced by `.golangci.yaml`.
- Add regression coverage for fixes.
- Keep each pull request focused on one concern.
- Update documentation when changing public behavior.
- Keep examples and fixtures free of credentials, private endpoints, customer names, and internal model identifiers.

## Reporting bugs

Open an issue with the version, operating system, reproduction steps, expected behavior, and actual behavior.

Report security issues privately as described in [SECURITY.md](SECURITY.md).
