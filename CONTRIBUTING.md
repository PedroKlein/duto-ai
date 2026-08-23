# Contributing

## Set up the repository

Install the pinned toolchain and run the deterministic gates:

```bash
mise install
mise run check
mise run integration
mise run scenarios
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for individual commands, process-contract checks, and test boundaries.

## Submit a change

1. Fork the repository.
2. Create a focused branch: `feature/` for new behavior, `fix/` for fixes, `docs/` for documentation, or `ci/` for CI and release automation.
3. Add behavior tests for code changes. A bug fix needs a regression test that fails without the fix.
4. Update public documentation when changing flags, schema fields, tools, provider seams, results, evidence, or host adapters.
5. Run the required gates.
6. Open a pull request. The project uses rebase merge.

Use this pull request body:

```markdown
## Summary

- Change one
- Change two

## Test plan

- [ ] `mise run check`
- [ ] `mise run integration`
- [ ] `mise run scenarios`
- [ ] Feature-specific verification
```

## Required checks

```bash
mise run check
mise run integration
mise run scenarios
go mod tidy -diff
git diff --check
```

Run live model or hosted acceptance only after deterministic checks pass and only when the change requires it.

## Change rules

- Follow Go conventions enforced by `.golangci.yaml`.
- Keep one concern per pull request.
- Test public behavior rather than implementation details.
- Use standard library and native ADK behavior before adding dependencies or abstractions.
- Keep exported APIs small.
- Do not add compatibility paths for removed pre-v1 syntax or commands.
- Do not mix M2 Action behavior, M3 mutation/publication, durable hosting, or release work into an M1 core change without explicit scope.

## Public-content rules

Examples, fixtures, documentation, comments, and release notes must remain provider-neutral.

Do not commit:

- credentials or secret values;
- private endpoints or internal model identifiers;
- customer or private organization names;
- workstation-specific absolute paths;
- private planning IDs, run IDs, transcripts, or temporary evidence bundles;
- generated coverage files or local `.env` files.

Complete YAML examples must pass the strict decoder. Relative Markdown links must resolve. Public tool names and command flags must match the current binary.

## Report bugs and security issues

Open a GitHub issue for ordinary bugs. Include the version, operating system, exact command, minimal input, expected behavior, actual behavior, and deterministic reproduction.

Report security issues privately as described in [SECURITY.md](SECURITY.md).
