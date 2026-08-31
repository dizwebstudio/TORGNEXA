# ADR-0139 — Архивирование партии Почты России

Status: Accepted

## Context

The Russian Post connector can form and hand off a batch, but its provider
archive operation was still fail-closed. The official API models this as
moving a formed batch to an archive; it is reversible through a separate
operation and is not the same as cancelling an individual backlog order.

## Decision

Admit `logistics.batches.archive` only for the qualified Russian Post runtime.
Register `POST /api/v1/logistics/batches/archive/{batch_id}` as an
authenticated, tenant-scoped operation. It requires an enabled account
capability, an approved `fulfillment.batch.archive` request bound to the
connector account and batch ID, and `Idempotency-Key`.

The host adapter calls the official `PUT /1.0/archive` endpoint with a JSON
array containing exactly one numeric batch name. It accepts only one response
item with no `error-code` and an exact `batch-name` match. The normalized
result is `ARCHIVED` with `archived=true`.

Use the existing tenant-scoped `operation_receipts` boundary. The durable
result contains only the normalized batch ID, status, archived flag and
observation time; raw provider responses are not persisted.

## Alternatives considered

Keeping archive fail-closed would leave formed-batch lifecycle management
incomplete. Calling a guessed cancellation endpoint would misrepresent the
provider contract and could be destructive. Sending the provider array from
the browser would bypass approval, tenant isolation and duplicate protection;
it is rejected.

## Consequences

Operators can archive an approved formed batch from the integration surface.
The operation is bounded to one numeric batch name and the provider result is
strictly matched. Restoring an archived batch remains unqualified until its
own contract and reconciliation behavior are reviewed.

## Security and privacy impact

Default-deny runtime admission, account capability, approval and idempotency
are independent gates. The request contains only a provider reference; no
customer PII crosses the adapter boundary.

## Compatibility impact

The additive capability, route, schemas and generated SDK method do not alter
batch formation or check-in. Existing accounts are unchanged until the new
capability is explicitly enabled.

## Migration and data impact

No migration is required. The existing operation receipt schema supports the
normalized archive result and pending/completed replay states.

## Operational impact

An ambiguous provider outcome remains pending and requires reconciliation
before another request is allowed. Live qualification requires a non-
production Russian Post account, current credentials and an evidence bundle
based on the official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
