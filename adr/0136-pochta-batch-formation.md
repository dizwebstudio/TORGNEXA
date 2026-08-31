# ADR-0136 — Формирование партий Почты России

Status: Accepted

## Context

The Russian Post adapter already had a bounded batch directory read and
single-order backlog operations. The official Otpravka API also provides a
batch-formation operation, but no host route admitted it; exposing it without
approval and durable replay protection could create duplicate postal batches.

## Decision

Admit `logistics.batches.create` only for the qualified Russian Post runtime.
Register `POST /api/v1/logistics/batches` as an authenticated, tenant-scoped
route. The handler requires the account capability, an approved
`fulfillment.batch.create` request whose resource ID is bound to the canonical
request digest, and `Idempotency-Key`. The adapter converts 1–100 unique
numeric order references into the official `POST /1.0/user/shipment` JSON
array, passing only the optional sending date and online-balance flag.

Use the existing tenant-scoped `operation_receipts` table for this external
mutation. A fresh receipt owns the provider call; a completed receipt is
replayed without a second call, while a pending receipt returns an accepted
pending response. Only the normalized batch identity, status, count and
observation time are stored as the receipt result.

## Alternatives considered

Keeping batch formation fail-closed would avoid a new remote write but would
leave the already documented provider operation unreachable. Calling it from
the UI without an approval and receipt boundary would risk duplicate batches,
so that alternative is rejected.

## Consequences

Operators can form a bounded Russian Post batch from existing backlog orders.
The result is an accepted remote batch projection, not proof that the batch
has entered postal processing; handoff remains a separate operation.

## Security and privacy impact

The operation is write-sensitive and remains default-deny until both runtime
support and account capability are enabled. Approval and idempotency are
independent gates. Provider order IDs are treated as remote references, are
validated as numeric only inside the Russian Post adapter and are not returned
as a durable host projection. No credential or raw provider body is persisted.

## Compatibility impact

The additive capability, API operation and generated SDK method do not change
existing shipment or batch-read contracts. Existing accounts remain unchanged
until an operator explicitly enables the new capability.

## Migration and data impact

No database migration is needed: the existing tenant-scoped
`operation_receipts` table already supports the pending/completed result
states. The new normalized result is bounded to the existing JSON receipt
limit and contains no provider payload.

## Operational impact

An ambiguous transport failure intentionally leaves the receipt pending and
must be resolved by reconciliation before another provider request is
allowed. Postal handoff/check-in and separate return shipment operations stay
outside this ADR. Live qualification still requires a non-production account,
current provider credentials and an evidence bundle.
