# ADR 0071: Payment and SBP provider boundary

## Status
Accepted — Task 71.

## Context
Task 073 needs payment create/status/refund/reconciliation and verified webhooks, with SBP as a reference adapter, while keeping raw card data out of TORGNEXA and preserving exact-money/idempotency semantics.

## Decision
1. Add capability-specific payment interfaces to Connector SDK v1.
2. Admit `sbp` as a reference provider through typed host-injected transport and certificate/credential references.
3. Use exact minor-unit amounts, explicit external/idempotency IDs and remote authoritative status.
4. Verify webhook authenticity in the transport before host acceptance and persist only delivery/body digest evidence.
5. Do not accept PAN/CVV/card-track data in payment contracts.

## Alternatives considered
- Build a card vault: rejected; outside TORGNEXA scope and compliance posture.
- Trust unverified webhook bodies: rejected.
- Retry ambiguous payment creation without reconciliation: rejected because it can duplicate money movement.

## Compatibility impact
The SDK change is additive; existing root Connector and finance contracts remain compatible. Payment-specific interfaces are optional capabilities.

## Migration and data impact
Migration `000046` adds tenant-scoped payment records and append-only webhook evidence. Existing settlement ledgers are not rewritten.

## Operational impact
Production must configure an approved SBP acquiring contract, certificate/secret rotation, webhook verification, timeout/reconciliation policy and payment SLOs.

## Security and privacy impact
No raw card data is modeled. Webhook bodies are represented by digests/evidence, secrets are host-owned and tenant isolation is enforced.

## Consequences
TORGNEXA gains a safe payment rail abstraction and an SBP baseline; additional acquiring implementations can follow Task 087 without altering consumers.
