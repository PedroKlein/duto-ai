# Security policy

## Supported versions

Security fixes are applied to the latest v0.x release. Older pre-1.0 releases are not maintained.

| Version | Supported |
|---|---|
| 0.2.x | Yes |
| 0.1.x | No |

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a public issue for suspected credential exposure, command injection, path traversal, token misuse, or network-access vulnerabilities.

Include:

- affected version and operating system
- minimal reproduction
- expected and observed behavior
- potential impact
- whether any credential or private endpoint was exposed

You should receive an acknowledgement within seven days.

## Deployment guidance

Workflow files and enabled tools are trusted code in v0.2.x.

- Grant `GITHUB_TOKEN` only the permissions required by selected tools.
- Do not run write, network, or shell tools on untrusted pull-request content.
- Pin `PedroKlein/duto-ai` to an exact release tag.
- Keep provider credentials in GitHub secrets or the runner's secret store.
- Review model-generated arguments before enabling mutation tools in future releases.

Tool whitelisting controls what the model can invoke. It does not isolate duto-ai from the runner operating system or network.

### High-risk tools

- `shell.run` executes arbitrary shell text as the runner user.
- `web.fetch` and `web.request` can access network locations reachable from the runner.
- `github.post-review`, `github.post-comment`, `github.add-labels`, `github.create-issue`, `github.edit-issue`, `github.merge-pr`, and `github.request-reviewers` can change repository state according to token permissions.

Use read-only defaults and add high-risk tools only to the steps that need them.
