# ADR-0140 — Возврат партии из архива Почты России

Status: Accepted

## Context

The Russian Post connector can move a formed batch to the provider archive.
The official API describes returning that batch from the archive as a
separate operation; it is not a cancellation of a backlog order or a new
batch formation request.

## Decision

Admit `logistics.batches.unarchive` only for the qualified Russian Post
runtime. Register `POST /api/v1/logistics/batches/archive/revert/{batch_id}`
as an authenticated, tenant-scoped operation. It requires an enabled account
capability, an approved `fulfillment.batch.unarchive` request bound to the
connector account and batch ID, and `Idempotency-Key`.

The host adapter calls the official `POST /1.0/archive/revert` endpoint with
a JSON array containing exactly one numeric batch name. It accepts only one
response item with no `error-code` and an exact `batch-name` match. The
normalized result is `RESTORED` with `archived=false`.

Use the existing tenant-scoped `operation_receipts` boundary. The durable
result contains only the normalized batch ID, status, archive flag and
observation time; raw provider responses are not persisted.

## Alternatives considered

Keeping restore fail-closed would leave the reversible formed-batch lifecycle
incomplete. Reusing the archive operation with a status switch would hide a
different provider contract and weaken audit lineage. Sending the provider
array from the browser would bypass approval, tenant isolation and duplicate
protection; it is rejected.

## Consequences

Operators can restore an approved formed batch from the Russian Post archive.
The operation is bounded to one numeric batch name and the provider result is
strictly matched. Other archive search/edit/delete operations remain closed.

## Security and privacy impact

Default-deny runtime admission, account capability, approval and idempotency
are independent gates. The request contains only a provider reference; no
customer PII crosses the adapter boundary.

## Compatibility impact

The additive capability, route, schemas and generated SDK method do not alter
batch formation or archive behavior. Existing accounts are unchanged until
the new capability is explicitly enabled.

## Migration and data impact

No migration is required. The existing operation receipt schema supports the
normalized restore result and pending/completed replay states.

## Operational impact

An ambiguous provider outcome remains pending and requires reconciliation
before another request is allowed. Live qualification requires a non-
production Russian Post account, current credentials and an evidence bundle
based on the official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
