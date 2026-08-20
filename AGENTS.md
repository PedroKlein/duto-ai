# Agent instructions

## GitHub project configuration

### Repository

- **Repo**: `PedroKlein/duto-ai`
- **Host**: `github.com`

### Issue tracking

- **Tracker**: GitHub Issues
- GitHub Issues **enabled**

### Merge method

- **rebase**
- Keep history linear and preserve reviewable commits.

### Branch naming

| Prefix | Use | Example |
|---|---|---|
| `feature/` | New behavior | `feature/provider-registry` |
| `fix/` | Bug fixes | `fix/output-validation` |
| `docs/` | Documentation only | `docs/workflow-reference` |
| `ci/` | CI, release, or Action changes | `ci/checksum-install` |

### Pull request body

```markdown
## Summary

- Change one
- Change two

## Test plan

- [ ] `mise run check`
- [ ] `mise run integration`
- [ ] Feature-specific verification
```

### CI runners

- Primary: `ubuntu-latest`
- Release artifacts: Linux and macOS, AMD64 and ARM64

### Pre-commit gates

```bash
mise run check
mise run integration
```

Run live smoke or duto-test acceptance only after deterministic gates pass.

## Code and documentation rules

- Follow the Go conventions enforced by `.golangci.yaml`.
- Add regression tests for behavior changes.
- Keep pull requests focused on one concern.
- Keep public examples, fixtures, docs, comments, and release notes provider-neutral.
- Never commit credentials, private endpoints, customer names, or internal model identifiers.
- Update the README and reference docs when public flags, schema fields, tools, providers, Action inputs, or Action outputs change.
