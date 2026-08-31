# ADR-0137 — Передача партии Почты России в работу

Status: Accepted

## Context

Task 184 made it possible to form a bounded Russian Post batch, but the
provider's next documented step — check-in of that batch and submission of the
electronic F103 form — remained fail-closed. A direct UI call would create a
duplicate or unauthorized postal submission unless it had the same approval,
tenant and durable replay boundary as other sensitive logistics writes.

## Decision

Admit `logistics.batches.submit` only for the qualified Russian Post runtime.
Register `POST /api/v1/logistics/batches/{batch_id}/submit` as an authenticated,
tenant-scoped route. The handler requires the account capability, an approved
`fulfillment.batch.submit` request whose resource ID is bound to the canonical
batch and option digest, and `Idempotency-Key`.

The adapter calls the official `POST /1.0/batch/{batch-name}/checkin` endpoint,
optionally sending `useOnlineBalance=true`. The provider response must confirm
`f103-sent`; the host returns only a normalized submission projection with the
exact batch reference, status, acceptance flag and observation time.

Use the existing tenant-scoped `operation_receipts` table. A fresh receipt owns
the provider call, a completed receipt is replayed without a second call, and
a pending receipt blocks automatic re-submission until reconciliation.

## Alternatives considered

Keeping check-in fail-closed would avoid the remote write but would leave the
formed-batch workflow incomplete. Calling check-in from the browser without an
approval and receipt would permit duplicate or untraceable F103 submission and
is rejected. Treating HTTP success alone as acceptance is rejected because the
provider response must explicitly confirm `f103-sent`.

## Consequences

Operators can hand an approved formed batch to Russian Post from the
integration surface. Submission is deliberately explicit and irreversible at
the provider boundary; a successful response means the provider accepted the
F103 handoff, not that physical dispatch has completed.

## Security and privacy impact

The write-sensitive operation is default-deny until runtime support and account
capability are enabled. Approval and idempotency are independent gates. Batch
references are validated as bounded remote identifiers; credentials stay in the
callback-scoped SecretProvider and no provider response body or recipient data
is persisted.

## Compatibility impact

The additive capability, route and generated SDK method do not change existing
batch formation, batch read or shipment contracts. Existing accounts remain
unchanged until an operator explicitly enables the new capability.

## Migration and data impact

No database migration is needed. The existing operation receipt schema stores
the bounded normalized submission projection and already supports pending and
completed replay states.

## Operational impact

An ambiguous result remains pending and requires provider reconciliation before
another submission is allowed. Disabling the capability prevents new calls but
does not erase receipt evidence. Live qualification requires a non-production
Russian Post account, current credentials and a recorded evidence bundle.
