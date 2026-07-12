# Development Setup

## Prerequisites

- [mise](https://mise.jdx.dev/) for toolchain management
- SAP AI Core credentials (for smoke tests)

## Quick Start

```bash
# Install toolchain
mise install

# Copy credentials from adk-provider-sapaicore (same AI Core instance)
cp /path/to/adk-provider-sapaicore/.env .env
# Or create from template:
cp .env.example .env
# Fill in your credentials

# Run all checks
mise run check
```

## Environment Variables

duto-ai uses the same AI Core credentials as `adk-provider-sapaicore`.
If you already have a `.env` for that project, copy it here:

```bash
cp ../adk-provider-sapaicore/.env .env
```

### Required for smoke tests

| Variable | Purpose |
|---|---|
| `AI_CORE_ENDPOINT` | SAP AI Core API endpoint |
| `AI_CORE_CLIENT_ID` | OAuth2 client ID |
| `AI_CORE_CLIENT_SECRET` | OAuth2 client secret |
| `AI_CORE_AUTH_URL` | OAuth2 token endpoint |
| `AI_CORE_RESOURCE_GROUP` | Resource group (orchestration mode) |

### Optional

| Variable | Purpose |
|---|---|
| `AI_CORE_FOUNDATION_DEPLOYMENT_ID` | Direct deployment ID (foundation mode) |
| `AI_CORE_FOUNDATION_MODEL` | Model name for foundation mode |
| `GITHUB_TOKEN` | GitHub PAT (smoke tests with real GitHub — optional) |

## Tasks

```bash
mise run build          # Build all packages
mise run test           # Unit tests with race detector
mise run lint           # golangci-lint
mise run check          # build + vet + lint + test (CI gate)
mise run integration    # Integration tests (mock LLM)
mise run smoke          # Smoke tests (real AI Core + mock GitHub)
mise run coverage       # Tests with coverage report
```

## Project Structure

See [docs/ARCHITECTURE.md](./ARCHITECTURE.md) for full architecture documentation.

## Credentials Source

Both `duto-ai` and `adk-provider-sapaicore` talk to the same SAP AI Core instance.
The `.env` file format is identical — credentials are interchangeable between projects.
