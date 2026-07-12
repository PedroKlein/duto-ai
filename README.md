# duto-ai

Composable AI building blocks for CI/CD pipelines.

**duto-ai** is a Go binary powered by [ADK Go v2](https://github.com/google/adk-go) that provides a standard runtime for adding AI to CI pipelines. Define deterministic execution graphs in YAML; the AI fills in the reasoning at each step. Model-agnostic, pipeline-agnostic, with fine-grained tool control.

## Why duto-ai?

There's no standard way for teams to add structured AI workflows to their CI/CD pipelines. Existing solutions are either opinionated single-purpose tools or give the AI full autonomy with minimal structure.

duto-ai takes a different approach: **you define the execution graph, the AI reasons within each node**. This gives you:

- **Predictability** — deterministic DAG execution, isolated steps, no hidden state
- **Security** — per-step tool whitelisting with dot-namespaced tools and glob patterns
- **Flexibility** — any LLM provider (SAP AI Core, OpenAI, Anthropic, self-hosted)
- **Familiarity** — YAML workflows with `steps:` and `needs:`, just like GitHub Actions

## Quick Start

```yaml
# .github/workflows/ai-review.yml
name: AI PR Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: PedroKlein/duto-ai@v0
        with:
          workflow: .github/ai-workflows/pr-review.yaml
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          AI_CORE_CLIENT_ID: ${{ secrets.AI_CORE_CLIENT_ID }}
          AI_CORE_CLIENT_SECRET: ${{ secrets.AI_CORE_CLIENT_SECRET }}
          AI_CORE_ENDPOINT: ${{ secrets.AI_CORE_ENDPOINT }}
```

## Workflow Definition

```yaml
# .github/ai-workflows/pr-review.yaml
name: PR Code Review

steps:
  - id: gather
    model: light
    tools: [github.read-pr, github.read-diff, github.list-changed-files]
    prompt: |
      Read the PR metadata and diff using the available tools.
      Summarize what this PR changes.
    output: context

  - id: analyze
    needs: [gather]
    model: heavy
    skills: [security-analysis]
    prompt: |
      Analyze the PR changes for security, performance, and convention issues.
      Identify specific problems with line references.
    output: findings

  - id: report
    needs: [analyze]
    model: medium
    tools: [github.post-review, github.post-comment, github.add-labels]
    prompt: |
      Post findings as inline review comments using the github.post-review tool.
```

Each step receives its predecessor's output automatically via ADK's workflow engine.
Steps with no mutual `needs:` run in **parallel** automatically.

## Global Config

```yaml
# .github/ai-workflows/config.yaml
provider:
  type: ai-core
  config:
    resource_group: ${AI_CORE_RESOURCE_GROUP}

models:
  light: gpt-4.1-mini
  medium: gpt-4.1
  heavy: claude-sonnet-4

defaults:
  model: medium
  model_config:
    temperature: 0.2
    max_tokens: 4096
  tools:
    - github.read-diff
    - github.read-pr
    - github.list-changed-files
    - files.read
    - files.find
    - files.grep

context_files:
  - AGENTS.md
  - CONTEXT.md
```

## Tool System

Tools are dot-namespaced (`category.action`) and whitelisted per step:

| Namespace | Examples |
|-----------|----------|
| `github.*` | `read-diff`, `read-pr`, `post-review`, `add-labels`, `merge-pr` |
| `git.*` | `log`, `blame`, `show`, `diff`, `commit` |
| `files.*` | `read`, `write`, `find`, `grep` |
| `web.*` | `search`, `fetch`, `request` |
| `security.*` | `search-vulnerabilities`, `check-dependencies` |
| `shell.*` | `run` (sandboxed: timeout + cwd lock) |

Glob patterns supported: `github.*`, `github.read-*`, `files.*`, `*`.

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Runtime | ADK Go v2 | Graph-based workflows, multi-agent, model-agnostic |
| Distribution | Composite action + pre-built binary | ~200ms startup, GHA tool cache |
| Triggering | None — GHA/pipeline owns it | Runtime-agnostic |
| Step isolation | Fully isolated (fresh context each) | Predictable cost and behavior |
| Step execution | Agentic loop (with hard limits) | Powerful but bounded |
| Outputs | Always text (strings) | Simple; typed outputs deferred |
| Failure | Fail-fast (whole workflow aborts) | Predictable, auditable |
| Provider contract | ADK's `model.LLM` interface | Plug any provider |
| Tool permissions | Configurable defaults + per-step whitelist | Security + convenience |

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  Pipeline Runtime (GHA, GitLab, local CLI)            │
│  provides: event context, secrets, env vars           │
└───────────────────┬──────────────────────────────────┘
                    │
                    ▼
┌──────────────────────────────────────────────────────┐
│  duto-ai binary                                       │
│  ┌────────────────────────────────────────────────┐  │
│  │ YAML Parser → DAG Compiler → ADK v2 Graph      │  │
│  │                                                 │  │
│  │ ┌─────────┐    ┌─────────┐    ┌─────────┐     │  │
│  │ │ Step 1  │───▶│ Step 2  │───▶│ Step 3  │     │  │
│  │ │(gather) │    │(analyze)│    │(report) │     │  │
│  │ └─────────┘    └─────────┘    └─────────┘     │  │
│  │  model: light   model: heavy   model: medium   │  │
│  │  tools: [...]   tools: [...]   tools: [...]    │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

## File Layout

```
.github/
  ai-workflows/
    config.yaml          # Global config (provider, models, defaults)
    pr-review.yaml       # Workflow definition
    issue-triage.yaml    # Another workflow
    prompts/             # Prompt template .md files
      gather-context.md
    skills/              # Behavioral .md files (reusable expertise)
      code-review.md
      security-analysis.md
```

## Local Development

```bash
# Test a workflow locally with a mock event
duto-ai run --event event.json .github/ai-workflows/pr-review.yaml
```

## Related

- [`adk-provider-sapaicore`](https://github.com/PedroKlein/adk-provider-sapaicore) — ADK Go model provider for SAP AI Core
- [ADK Go v2](https://github.com/google/adk-go) — The agent runtime powering duto-ai
