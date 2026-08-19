# ADR 0079: YooKassa as production reference acquirer

## Status
Accepted

## Context
Task 087 must prove the generic PaymentProvider against a production card/acquiring API in addition to SBP while preserving the PCI boundary.

## Decision
Admit YooKassa behind existing PaymentProvider capabilities for create, status, refund, webhook and reconciliation. POST idempotency remains caller supplied and ambiguous results are reconciled rather than blindly replayed.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Capturing PAN/CVV or implementing a custom card form was rejected. Provider-specific payment state in Core was rejected.

## Compatibility impact
Payment SDK v1 remains unchanged and existing SBP behavior is unaffected.

## Migration and data impact
No new provider-specific payment table is required; existing generic payment/webhook/settlement evidence is reused.

## Security and privacy impact
Credentials remain opaque; PAN/CVV are outside TORGNEXA, callback replay is deduplicated and only safe payment references are persisted.

## Operational impact
Operators use provider test shops for live qualification and reconcile HTTP 500/outcome-unknown cases with the same idempotency key or status reads.
