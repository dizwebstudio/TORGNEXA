# ADR-0142 — Удаление отдельной возвратной отправки Почты России

Status: Accepted

## Context

The Russian Post API exposes a dedicated delete operation for a standalone
return shipment. Treating any successful HTTP status as proof of deletion
would hide provider error objects, while allowing a caller to supply a remote
URL or an arbitrary payload would weaken the connector egress and approval
boundaries.

## Decision

Admit `logistics.return.separate.delete` only for the qualified Russian Post
runtime. Register
`DELETE /api/v1/logistics/returns/separate/{return_id}` as an authenticated,
tenant-scoped and approval-bound operation with a required idempotency key.

The host adapter calls the official
`DELETE /1.0/returns/delete-separate-return?barcode=...` endpoint over fixed
HTTPS egress. It sends no request body, accepts a `2xx` response only when the
body is empty or its JSON `code` is empty, and rejects every provider error
code. The normalized result is `DELETED` with `deleted=true` and the exact
barcode; no provider payload is persisted.

## Alternatives considered

Reusing a generic shipment cancellation route would conflate provider state
and expose the wrong capability. Accepting a non-empty response as success
would turn `RETURN_SHIPMENT_NOT_FOUND` or an illegal-state response into a
false deletion. Allowing arbitrary provider URLs or barcodes outside the
validated path would violate fixed egress and bounded reference rules; these
alternatives are rejected.

## Consequences

Operators can remove a disposable standalone return from the integration
surface after approval, and completed retries are safe. The action is
irreversible at the provider and remains unavailable for connectors without an
explicit runtime qualification.

## Security and privacy impact

The capability is write-sensitive. Authentication, tenant account resolution,
account capability, matching approval, fixed HTTPS egress, barcode validation
and operation idempotency are independent gates. No recipient address, name,
credential or raw provider response enters the operation receipt.

## Compatibility impact

The additive capability, route and generated SDK method do not alter existing
return creation or shipment cancellation. Existing accounts remain unchanged
until the capability is enabled.

## Migration and data impact

No database migration is required. The operation receipt stores only the
normalized deletion acknowledgement and is scoped to the authenticated
organization/workspace.

## Operational impact

Provider errors are mapped to the existing logistics error surface. A timeout
must remain pending until reconciliation; operators must not blindly retry an
unknown provider result. Live qualification requires a non-production test
return and the official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
