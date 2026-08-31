# ADR-0143 — Редактирование отдельной возвратной отправки Почты России

Status: Accepted

## Context

The Russian Post API exposes a POST operation for editing a standalone return
shipment. A successful HTTP status alone is insufficient because provider
responses can contain validation errors or an unexpected return identity.

## Decision

Admit `logistics.return.separate.edit` only for the qualified Russian Post
runtime. Register
`POST /api/v1/logistics/returns/separate/{return_id}` as an authenticated,
tenant-scoped and approval-bound operation with a required idempotency key.

The host adapter calls the official
`POST /1.0/returns/{barcode}` endpoint over fixed HTTPS egress. It sends only
the bounded editable standalone-return fields and accepts the response only
when it has no errors and confirms the exact requested `return-barcode`.
The normalized result is `UPDATED` with `updated=true`; provider payload,
addresses and names do not cross the host receipt boundary.

## Alternatives considered

Reusing the standalone-return create route would conflate provider identity
and could create a second return. Treating any `2xx` response as success
would hide provider validation errors. Accepting a caller-provided provider
URL or an unbounded payload would violate fixed egress and connector-boundary
rules; these alternatives are rejected.

## Consequences

Operators can correct an approved standalone return without changing its
provider identity, and completed retries are safe. The action remains
unavailable for connectors without explicit runtime qualification.

## Security and privacy impact

The capability is write-sensitive. Authentication, tenant resolution,
permission, capability, fixed HTTPS egress, barcode validation, matching
approval and operation idempotency are independent gates. No recipient
address, name, credential or raw provider response enters the operation
receipt.

## Compatibility impact

The additive capability, route and generated SDK method do not alter existing
return creation, deletion or shipment cancellation. Existing accounts remain
unchanged until the capability is enabled.

## Migration and data impact

No database migration is required. The normalized result uses the existing
tenant-scoped operation receipt and stores only the confirmed barcode,
`UPDATED` status, boolean acknowledgement and observation time.

## Operational impact

Provider errors are mapped to the existing logistics error surface. A timeout
must remain pending until reconciliation; operators must not blindly retry an
unknown provider result. Live qualification requires a non-production test
return and the official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
