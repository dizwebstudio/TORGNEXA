# ADR-0138 — Отдельная возвратная отправка Почты России

Status: Accepted

## Context

The Russian Post connector already creates returns for an existing RPO, but
the official API also documents a separate-return operation for cases where no
direct shipment barcode exists. Leaving that operation fail-closed makes the
return workflow incomplete; sending it directly from the browser would bypass
approval, tenant isolation and durable duplicate protection.

## Decision

Admit `logistics.return.separate.create` only for the qualified Russian Post
runtime. Register `POST /api/v1/logistics/returns/separate` as an authenticated,
tenant-scoped operation. It requires an enabled account capability, an approved
`fulfillment.return.separate.create` request bound to a digest of all options,
and `Idempotency-Key`.

The host adapter calls the official `PUT
/1.0/returns/return-without-direct` endpoint with exactly one item. It maps
the neutral address to the documented Russian Post address shape, converts
minor-unit declared value to whole roubles, and accepts only one response with
`position=0`, no errors and a valid `return-barcode`.

Use the existing tenant-scoped `operation_receipts` boundary. The durable
result contains only the normalized remote ID, status, tracking number and
observation time; names and addresses are request-scoped and are not persisted
in the receipt or logged.

## Alternatives considered

Keeping the operation closed would leave standalone returns unavailable. Using
the existing direct-RPO return contract would incorrectly require a barcode
that the standalone operation is designed to avoid. Sending the provider's
array directly from the browser would permit duplicates and unapproved remote
writes; it is rejected.

## Consequences

Operators can create an approved standalone return from the integration
surface. The operation is bounded to one item and explicit Russian addresses;
provider batch arrays and raw response details do not cross the host boundary.

## Security and privacy impact

Default-deny runtime admission, account capability, approval and idempotency
are independent gates. Names and addresses are PII and remain in memory only
for the request-scoped adapter call; the approval resource and receipt use a
one-way digest and normalized result only.

## Compatibility impact

The additive capability, route, schemas and generated SDK method do not alter
the existing direct-RPO return or shipment contracts. Existing accounts are
unchanged until the new capability is explicitly enabled.

## Migration and data impact

No migration is required. The existing operation receipt schema is sufficient
for the normalized standalone-return result and replay states.

## Operational impact

An ambiguous provider outcome remains pending and requires reconciliation before
another request is allowed. Live qualification requires a non-production
Russian Post account, current credentials and an evidence bundle based on the
official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
