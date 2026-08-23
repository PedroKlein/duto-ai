# ADR 001: Provider and model policy

- **Status:** Accepted; M1 implementation shipped
- **Date:** 2026-08-25
- **ADK baseline:** `google.golang.org/adk/v2` v2.2.0 (`b264039aaec43baedc123e5b9a0cf87681d0bbca`)
- **Scope:** Trusted model bindings, portable aliases, the compiler seam, lifecycle, errors, and adapter conformance

## M1 implementation amendment

The shipped composition root implements the consumer-owned resolver, strict aliases, standard generation fields, and run-local model reuse. It currently normalizes timeout behavior used by native retry. The broader provider error taxonomy and public multi-adapter conformance surface remain contract guidance for future adapters, not additional shipped CLI behavior.

## Context

Workflows must be portable while provider credentials, endpoints, and concrete model targets remain trusted runtime data. Duto already executes through ADK `model.LLM`; duplicating that protocol would add no useful seam.

The superseded implementation accepted an unmapped workflow model string as a concrete target and parsed provider-specific extras without forwarding them. V1 removes both ambiguous behaviors.

## Decision drivers

- Workflow YAML never contains credentials, endpoints, provider types, or concrete targets.
- Model selection is finite and inspectable before construction.
- The compiler depends on ADK, not a provider SDK.
- The selected seam must support the bundled adapter and deterministic fake models without a speculative public registry.
- Standard model configuration stays provider-neutral.
- Cancellation, tool schemas, structured output, usage, and streaming retain native ADK semantics.

## Alternatives

### A. Consumer-owned resolver

```go
package compiler

import (
    "context"

    "google.golang.org/adk/v2/model"
)

type ModelSelection struct {
    Alias string
}

type ModelResolver func(context.Context, ModelSelection) (model.LLM, error)
```

The trusted composition root closes over validated bindings and any run-local cache. The compiler knows only the logical alias and returned ADK model.

### B. Public provider registry

A public registry would expose provider factories, raw configuration, lifecycle, and registration rules. ADK v2.2.0 has `model.Register`/`model.NewLLM`, but that registry is process-global, pattern-based, panic-on-invalid-registration, and oriented around concrete model names. It does not enforce duto workflow aliases or trusted configuration ownership.

### C. Duto model protocol

A duto-owned request/response protocol would mirror `model.LLM`, tool schemas, content parts, usage, finish reasons, errors, and streaming, then require a bridge back to ADK.

| Concern | A: resolver | B: registry | C: parallel protocol |
|---|---|---|---|
| Caller interface | One function | Factories and instances | Full model protocol |
| ADK fidelity | Native | Native after construction | Translation required |
| Workflow policy | Closed aliases | Separate policy still required | Separate policy still required |
| Current need | Yes | No second implementation proves it | No |
| Complexity | Small | Medium | Large |

## Decision

Select **A: a consumer-owned resolver returning ADK `model.LLM`**.

Do not create a public provider registry or multi-interface catalog in v1. The runtime may use private functions and a run-local map to construct/reuse the current adapter. Add a private factory seam only when a second construction path proves one is needed.

ADK's global model registry is not the workflow policy seam. A trusted host may use it internally later, but portable workflows still resolve only duto aliases.

## Configuration ownership

Trusted runtime configuration owns concrete resources:

```yaml
version: 1
providers:
  default:
    type: built-in
    config:
      credential: "${MODEL_CREDENTIAL}"
models:
  light:
    provider: default
    target: model-a
  capable:
    provider: default
    target: model-b
```

Portable workflow YAML owns only logical selection and standard ADK generation fields:

```yaml
model: capable
model_config:
  temperature: 0.2
  max_output_tokens: 4096
```

Rules:

1. Every referenced alias must exist in trusted runtime configuration.
2. Unknown aliases fail before a provider or model is constructed.
3. A workflow string is never passed through as a concrete target.
4. Credentials, endpoint values, provider configuration, and concrete targets are omitted from prompts, diagnostics, effective-plan output, cache keys, and evidence.
5. `temperature` and `max_output_tokens` compile to ADK `GenerateContentConfig` request fields.
6. Provider-specific workflow `extra` values are not part of v1. Adapter-specific configuration remains trusted-only until a ranked use case proves a portable field is needed.

This removes the previous admission-map and cache-identity machinery for unused generic extras.

## Lifecycle

- Validate workflow, graph, tools, trust, and aliases before model construction.
- Construct models lazily through the resolver.
- Reuse a resolved alias within one run only when the adapter supports concurrent calls.
- Do not keep credentials or models in a process-global duto cache.
- The run context governs construction; ADK supplies the request context for every model call.
- Consume ADK iterators completely during normal execution before closing run-scoped resources.

The implementation may be a closure over one concrete run-scoped struct; it does not require exported `Catalog`, `Factory`, `Instance`, or `Plan` types.

## Stable errors

Duto normalizes only categories callers use:

```go
type ErrorKind uint8

const (
    ErrorUnknown ErrorKind = iota
    ErrorInvalidConfig
    ErrorUnauthorized
    ErrorRateLimited
    ErrorUnavailable
    ErrorTimeout
    ErrorProtocol
)
```

An error carries a kind, optional safe retry delay, and wrapped cause. Retryability is derived by policy from the kind; it is not stored as a second boolean that can disagree.

- Use `errors.Is` for cancellation and deadlines.
- Use `errors.As` for normalized provider errors.
- Return or yield an error once; do not both log and return it.
- Provider-specific strings are diagnostic data, never control flow.
- Valid model refusals remain terminal ADK responses when the provider represents them that way.

## Adapter conformance

Every bundled adapter must pass one ADK-level suite:

| Area | Required behavior |
|---|---|
| Text | Preserve roles, ordered non-thought parts, finish metadata, and model identity. |
| Tools | Read declarations from `req.Config.Tools`; support JSON-schema declarations and correlated calls/results. |
| Structured output | Honor ADK response schema behavior, including v2.2.0 `OutputSchema` with tools. |
| Usage | Preserve reported usage, including final streaming usage; unavailable metrics remain absent, never fabricated as zero. |
| Cancellation | Propagate context through construction, retries, requests, and stream reads; stop promptly. |
| Streaming | Yield ordered partials and one terminal response without duplicated accumulated text. |
| Errors | Normalize the small stable taxonomy while preserving wrapped causes. |
| Concurrency | Shared run-local instances do not leak or race request state. |

Provider capability differences are handled by conformance and preflight, not by exposing provider details to workflows.

## M1 implementation

The shipped composition root supports one bundled adapter behind the private resolver. Strict admission requires every workflow alias to map to a trusted provider and target before construction. Runtime instances are cached by alias for one invocation only.

The portable model configuration is limited to `temperature` and `max_output_tokens`. Unknown aliases and provider-specific workflow fields reject; there is no compatibility conversion or concrete-target pass-through.

## Rejected alternatives

- **Public registry now:** no ranked use case or second adapter proves the surface.
- **Duto model protocol:** duplicates the selected runtime and loses ADK fidelity.
- **Concrete workflow targets:** break portability and finite policy.
- **Global duto model cache:** risks stale credentials and cross-run state.
- **Portable arbitrary extras:** unused by current scenarios and expensive to secure correctly.
- **Another native adapter for parity:** brand coverage is not a requirement.

## Consequences

### Positive

- The compiler seam is one function.
- Provider SDKs and secrets stay below the composition root.
- Native ADK tool, schema, usage, cancellation, and streaming behavior remains intact.
- Unused provider-extension machinery is removed from M1.

### Negative

- Provider-specific workflow tuning waits for a real portable requirement.
- A trusted custom host can bypass bundled-adapter guarantees when injecting its own resolver.
- ADK v2.2.0 behavior must be pinned by conformance tests before the dependency upgrade is accepted.
