# Contributing

Contributions are welcome! Here's how to get started.

## Development

This project uses [mise](https://mise.jdx.dev/) for toolchain management. Run `mise install` to get the correct Go and tooling versions.

```bash
mise install
mise run check  # build + vet + lint + test
```

Or run individual tasks:

```bash
mise run build
mise run lint
mise run test
mise run integration  # integration tests with mock LLM
mise run smoke        # smoke tests (requires SAP AI Core credentials)
```

## Submitting changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes with tests
4. Ensure all checks pass locally (`mise run check`)
5. Submit a pull request

## Guidelines

- Follow existing code style (enforced by golangci-lint)
- Add tests for new functionality
- Keep PRs focused — one feature or fix per PR
- Update docs if you change the public API or workflow schema

## Reporting bugs

Open an issue with:
- Go version and OS
- Minimal reproduction steps
- Expected vs actual behavior
