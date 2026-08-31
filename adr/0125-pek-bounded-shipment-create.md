# ADR-0125 — ПЭК bounded shipment create runtime

Status: Accepted

## Context

ПЭК documents a preregistration endpoint, but its payload also supports
different order types, delivery modes and later formed-cargo operations. A
generic shipment creator would therefore risk creating a real carrier request
with an unverified warehouse, party or delivery interpretation.

## Decision

Admit only the narrow self-delivery preregistration path through the existing
provider-neutral `LogisticsShipmentCreator`. The adapter sends one Russian
`orderType=0` request with `docflowType=FFS`, cargo `type=3`, service
`pek_type_3`, a tenant-configured sender warehouse and up to 50 parcels.
Sender legal data is non-secret tenant configuration; Basic credentials remain
callback-scoped in `SecretProvider`.

The host validates request bounds and the adapter accepts only a response with
a valid document identifier and exactly one numeric cargo code. The returned
provider acceptance is an asynchronous receipt and is not marked as
reconciled. Formed-cargo cancellation, returns, address delivery and batch
print forms remain unadmitted.

## Consequences

The approval-bound generic shipment route can submit a precisely bounded ПЭК
preregistration. Operators must configure the sender warehouse and legal data;
later status reads remain the source of remote convergence. Extending the
surface requires a separate provider qualification and contract review.

## Security and privacy impact

Secret credentials do not enter connector configuration, requests outside the
host transport or events. Sender and recipient contacts are request-scoped,
validated and not persisted by the adapter. Warehouse and cargo identifiers
remain remote references.

## Compatibility impact

The existing shipment-create SDK and API worker route are reused. Runtime
support and generated catalogs gain one additive capability for ПЭК; no
migration or provider-neutral schema change is introduced.

## Operational impact

Writes remain subject to existing tenant scope, capability, policy, approval,
idempotency, retry/DLQ and reconciliation handling. A provider `OK`/accepted
response must be followed by tracking or reconciliation before operational
completion is asserted.
