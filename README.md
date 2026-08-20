# duto-ai

Composable AI workflow steps for CI pipelines.

`duto-ai` compiles a YAML workflow into an [ADK Go v2](https://github.com/google/adk-go) execution graph. The workflow controls step order, model selection, tool access, retries, and timeouts. The model handles the reasoning inside each step.

Current release: **v0.2.2**

## What works today

- Sequential and parallel DAG execution through `steps` and `needs`
- An isolated ADK agent for each step
- Per-step model selection and generation settings
- Explicit tool whitelists with namespace globs
- Prompt files, context files, skills, and template variables
- Retry, timeout, and iteration limits
- Text, JSON, Markdown, file, and GitHub step-summary output
- GitHub Action and local CLI execution
- One bundled model-provider adapter

Provider registration, custom tools, typed outputs, and additional provider adapters are not public extension points in v0.2.2.

## Quick start

### 1. Add the provider configuration

Copy `.github/ai-workflows/config.yaml` into your repository. Replace the example model names and provide the referenced secrets as environment variables.

```yaml
provider:
  type: ai-core
  config:
    endpoint: ${DUTO_PROVIDER_ENDPOINT}
    resource_group: ${DUTO_PROVIDER_RESOURCE_GROUP}
    client_id: ${DUTO_PROVIDER_CLIENT_ID}
    client_secret: ${DUTO_PROVIDER_CLIENT_SECRET}
    auth_url: ${DUTO_PROVIDER_AUTH_URL}

models:
  light: example-small-model
  medium: example-medium-model

defaults:
  model: medium
  model_config:
    temperature: 0.2
    max_tokens: 4096
  tools:
    - github.read-pr
    - github.read-diff
    - github.list-changed-files
```

The environment variable names are user-defined. They are expanded before the configuration is parsed.

### 2. Define a workflow

Create `.github/ai-workflows/pr-review.yaml`:

```yaml
name: PR review

steps:
  - id: gather
    model: light
    tools:
      - github.read-pr
      - github.read-diff
      - github.list-changed-files
    prompt: |
      Read the pull request and summarize the change.
    output: context

  - id: review
    needs: [gather]
    model: medium
    tools: []
    prompt: |
      Review the preceding step output for correctness and security issues.
    output: findings

  - id: report
    needs: [review]
    tools:
      - github.post-review
    prompt: |
      Post the findings as a pull request review.
```

ADK passes predecessor output to successor steps. In v0.2.2, `output` enables step output storage, but its value is not yet a user-addressable output name.

### 3. Run it from GitHub Actions

Create `.github/workflows/ai-review.yaml`:

```yaml
name: AI review

on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: PedroKlein/duto-ai@v0.2.2
        with:
          workflow: .github/ai-workflows/pr-review.yaml
          config: .github/ai-workflows/config.yaml
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          DUTO_PROVIDER_ENDPOINT: ${{ secrets.DUTO_PROVIDER_ENDPOINT }}
          DUTO_PROVIDER_RESOURCE_GROUP: ${{ secrets.DUTO_PROVIDER_RESOURCE_GROUP }}
          DUTO_PROVIDER_CLIENT_ID: ${{ secrets.DUTO_PROVIDER_CLIENT_ID }}
          DUTO_PROVIDER_CLIENT_SECRET: ${{ secrets.DUTO_PROVIDER_CLIENT_SECRET }}
          DUTO_PROVIDER_AUTH_URL: ${{ secrets.DUTO_PROVIDER_AUTH_URL }}
```

Pin the Action to an exact release. The repository does not currently publish a moving `v0` tag.

## CLI

```text
duto-ai run [flags] workflow.yaml
duto-ai version
```

Run a local validation without calling a model:

```bash
duto-ai run --dry-run \
  --config .github/ai-workflows/config.yaml \
  .github/ai-workflows/pr-review.yaml
```

Important `run` flags:

| Flag | Default | Description |
|---|---:|---|
| `--config` | `.github/ai-workflows/config.yaml` | Global configuration path |
| `--repo` | environment | Repository override in `owner/repo` form |
| `--pr` | environment | Pull request number override |
| `--event` | environment | Event JSON file override |
| `--dry-run` | `false` | Validate and print the execution plan |
| `--log-level` | `info` | `debug`, `info`, `warn`, or `error` |
| `--verbose` | `false` | Enable debug logging |
| `--output-format` | `text` | `text`, `json`, or `markdown` |
| `--output-file` | empty | Also write formatted output to a file |

## Workflow fields

| Field | Required | Description |
|---|---:|---|
| `name` | yes | Workflow name |
| `steps` | yes | One or more workflow steps |
| `steps[].id` | yes | Unique step ID |
| `steps[].needs` | no | Predecessor step IDs |
| `steps[].model` | no | Model alias or model name |
| `steps[].model_config` | no | `temperature` and `max_tokens` overrides |
| `steps[].tools` | no | Tools added to defaults; `[]` disables all tools |
| `steps[].skills` | no | Skill names or Markdown file paths |
| `steps[].system` | no | Additional system instruction |
| `steps[].prompt` | yes | Inline prompt or `.md`/`.txt` path |
| `steps[].output` | no | Enables output storage for the step |
| `steps[].max_iterations` | no | Agent-loop limit; default `25` |
| `steps[].timeout` | no | Step timeout; default `300s` |
| `steps[].retry` | no | Transient-error retry settings |

Known v0.2.2 contract limits:

- The workflow-level `config` field is parsed but the CLI or Action config path controls the loaded file.
- `model_config.extra` is parsed but not forwarded to the provider.
- Output values are strings, and declared output names are not available to templates.
- Unknown tool names and unmatched globs are currently ignored.

## Tool catalog

Tools are available only when defaults or the current step whitelist them.

### GitHub

- Read: `github.read-issue`, `github.read-pr`, `github.read-diff`, `github.list-changed-files`, `github.read-comments`, `github.read-reviews`, `github.read-checks`, `github.search-issues`
- Write: `github.post-review`, `github.post-comment`, `github.add-labels`, `github.create-issue`, `github.edit-issue`, `github.merge-pr`, `github.request-reviewers`

### Repository files and history

- `files.read`, `files.find`, `files.grep`
- `git.log`, `git.blame`, `git.show`, `git.diff`

### Process and network

- `shell.run`
- `web.fetch`, `web.request`

Glob patterns such as `github.*`, `github.read-*`, `files.*`, and `*` are supported.

## Security model

Tool whitelisting limits what the model can call. It is not an operating-system sandbox.

- `shell.run` executes an arbitrary shell command with a fixed working directory and timeout.
- `web.fetch` and `web.request` can access network locations reachable from the runner.
- GitHub write tools use the permissions granted to `GITHUB_TOKEN`.
- Workflow files and enabled tools must be treated as trusted code.
- Do not expose write, network, or shell tools to workflows originating from untrusted pull requests.

Grant the job only the GitHub permissions required by its selected tools. See [SECURITY.md](SECURITY.md) for reporting and deployment guidance.

## Action inputs and outputs

The composite Action supports Linux and macOS runners on AMD64 and ARM64.

Inputs: `workflow`, `config`, `log-level`, `output-format`, `output-file`, `verbose`, and `version`.

Outputs: `status`, `workflow`, `duration-ms`, and `failed-step`.

The Action downloads the requested release into `RUNNER_TEMP`, verifies it against `checksums.txt`, and adds the versioned directory to `GITHUB_PATH`.

## Development

```bash
mise install
mise run check
mise run integration
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for the complete command list and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for implementation details.

## License

Licensed under the [Apache License 2.0](LICENSE).
