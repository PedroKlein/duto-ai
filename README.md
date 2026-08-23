# duto-ai

A CLI and runtime for bounded AI workflow DAGs.

`duto-ai` validates and compiles a YAML workflow into an [ADK Go v2](https://github.com/google/adk-go) execution graph. Humans, coding agents, planners, and CI jobs can produce workflows; duto executes the resulting finite, inspectable plan and returns structured results.

Local CLI execution is the primary interface. GitHub Actions is an official first-class host adapter over the same host-neutral runtime, not a separate workflow model.

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

## Accepted CLI-first contract

The next contract makes local one-shot use primary. Its exact command form is `duto-ai validate|plan|run [--config FILE] [--format text|json] WORKFLOW|-`; `-` reads workflow YAML from stdin.

```bash
duto-ai validate --config duto.yaml workflows/review.yaml
duto-ai plan --format json --config duto.yaml workflows/review.yaml > plan.json
duto-ai run --format json --config duto.yaml workflows/review.yaml > result.json
```

Each command emits one payload on stdout and sends logs or incidental diagnostics to stderr. `run` validates internally and uses fresh in-memory services by default. GitHub Actions will invoke the same JSON contract as the M2 host adapter; workspace/Git mutation and trusted publication belong to M3. Durable pause/resume and recovery are future-host capabilities, not M1 dependencies.

The accepted portable contract uses logical model aliases and provider-neutral workflow YAML. Trusted runtime configuration binds those aliases to a configured adapter; for example:

```yaml
provider:
  type: custom-provider
  config:
    endpoint: ${DUTO_PROVIDER_ENDPOINT}
    credential: ${DUTO_PROVIDER_CREDENTIAL}
models:
  light: example-small-model
  capable: example-capable-model
```

The accepted workflow, tool-profile, prompt/result, and milestone details are recorded in [ADRs 006–008](docs/adr/006-workflow-v1-contract.md).

## Current v0.2.2 quick start

The remainder of this section describes shipped v0.2.2 behavior, not the accepted next contract.

### 1. Define a workflow

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

### 2. Run it from GitHub Actions

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
          DUTO_PROVIDER_CREDENTIAL: ${{ secrets.DUTO_PROVIDER_CREDENTIAL }}
```

Pin the Action to an exact release. The repository does not currently publish a moving `v0` tag.

## Current v0.2.2 CLI

The shipped release currently exposes:

```text
duto-ai run [flags] workflow.yaml
duto-ai version
```

Its legacy validation path is the `run` command's dry-run flag. The accepted replacement is the separate `validate`, `plan`, and one-shot `run` surface shown above.

Important shipped `run` flags:

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

## Current v0.2.2 workflow fields

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

The lists below are the shipped v0.2.2 compatibility names. They are not aliases for the accepted M1 catalog.

### GitHub

- Read: `github.read-issue`, `github.read-pr`, `github.read-diff`, `github.list-changed-files`, `github.read-comments`, `github.read-reviews`, `github.read-checks`, `github.search-issues`
- Write: `github.post-review`, `github.post-comment`, `github.add-labels`, `github.create-issue`, `github.edit-issue`, `github.merge-pr`, `github.request-reviewers`

### Repository files and history

- `files.read`, `files.find`, `files.grep`
- `git.log`, `git.blame`, `git.show`, `git.diff`

### Process and network

- `shell.run`
- `web.fetch`, `web.request`

The accepted M1 replacement uses these exact names:

- Files: `files.read`, `files.find`, `files.grep`
- Git: `git.read.log`, `git.read.blame`, `git.read.show`, `git.read.diff`
- GitHub: `github.read.issue`, `github.read.pr`, `github.read.diff`, `github.read.changed-files`, `github.read.comments`, `github.read.reviews`, `github.read.checks`, `github.read.search-issues`
- Process and network: `shell.run`, `web.fetch`

M1 includes no GitHub write tool or arbitrary-method web request. Portable selectors may be exact names or the allowed terminal namespace wildcards `files.*`, `git.read.*`, and `github.read.*`. The broader `git.*`, `github.*`, and global `*` selectors reject with status 3 and zero model/tool calls. Trusted ceilings and parent authority can only narrow the resulting set. Step outputs and the terminal workflow result are typed.

## Security model

Tool whitelisting limits what the model can call. It is not an operating-system sandbox. The first bullets describe the accepted target contract; shipped v0.2.2 does not yet implement all of these guards.

- Accepted M1 `shell.run` is opt-in compatibility authority bounded by trusted command, workspace, environment, time, output, and call ceilings; it is not a sandbox.
- Accepted M1 `web.fetch` can access only locations admitted by trusted network policy; the shipped v0.2.2 `web.request` surface is not part of M1.
- Local callers may use GitHub read/review tools through trusted bindings; the invocation host does not own the tool.
- File/Git/GitHub mutation and trusted publication are M3 capabilities and are not granted merely by a token being present.
- Workflow files and enabled capabilities must be treated according to their trust context.
- With shipped v0.2.2, do not expose write, network, or shell tools to workflows originating from untrusted pull requests.

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
