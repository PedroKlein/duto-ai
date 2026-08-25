# ADR 010: M2 delivery completion and M3 entry

- **Status:** Accepted; M2 shipped
- **Date:** 2026-08-25
- **Scope:** M2 implementation status, contract integrity, and the transition to M3

## Context

[ADR 009](009-one-shot-github-action.md) froze the M2 Action contract before implementation. Its status line therefore records the state at the time of the contract freeze. Changing that line would also change the sealed contract bytes.

The implementation, deterministic verification, release packaging, and hosted acceptance are now complete. A separate record is needed so the delivery status stays current without rewriting the contract used to verify M2.

## Decision

M2 is shipped. The supported, hosted-verified distribution pair is:

- Action revision `462e48601658765e96448a147fbcd029f034e329`;
- binary release `v0.3.1`.

The Action source revision and installed binary release are independently pinned. Together they satisfy ADR 009's input, output, event, trust, checkout, installer, projection, and evidence requirements. M2 remains a one-shot adapter over the M1 runtime and does not include mutation, publication, or durable execution.

## Contract integrity

ADR 009 remains byte-for-byte sealed at SHA-256 `f4f9383040599afe51550bd86ed82a91d8f8515d15656e2b1482a771fdf18901`. This record changes implementation status only. It does not amend the M2 contract.

## M3 entry

M3 is the next unimplemented milestone. Its accepted scope remains:

- trust resolution, mutation authority, safe-output requests, and publication invariants from [ADR 003](003-trust-and-safe-outputs.md);
- bounded authoring and delivery-layer ownership from [ADR 008](008-product-center-and-delivery-layers.md).

M3 implementation begins separately. It must preserve M1 and M2 behavior and establish deterministic negative security coverage before opening mutation or publisher boundaries. Durable sessions, cross-runner recovery, asynchronous reply correlation, and open-ended planning remain outside M3.

## Consequences

- M1 and M2 are shipped delivery layers.
- Consumers can use the verified Action and binary pair while M3 is developed.
- M3 can proceed without reopening the sealed M2 contract.
- This status update does not publish a new binary release or add M3 behavior.
