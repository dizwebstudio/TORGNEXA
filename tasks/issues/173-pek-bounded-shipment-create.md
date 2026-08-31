# Task 173: ПЭК bounded shipment create

## Status

`repository-complete` — 2026-08-31.

## Objective

Expose the documented ПЭК preregistration operation through the existing
provider-neutral logistics shipment creator without guessing formed-cargo,
return or address-delivery semantics.

## Deliverables

- tenant-scoped non-secret sender configuration with strict validation;
- official `/api/v1/preregistration/submit/` mapping for one self-delivery order;
- runtime registry and generated support admission for `logistics.shipment.create`;
- strict `documentId`/single numeric `cargoCode` response validation;
- deterministic adapter and host-transport tests for success and malformed input;
- synchronized matrix, connector docs, task record, ADR and architecture review.

## Scope limits

Only Russian `orderType=0` preregistration with service `pek_type_3`, one
configured sender warehouse and at most 50 parcels is admitted. The provider's
acceptance is asynchronous and is not treated as reconciliation. Formed-cargo
cancellation, returns, address delivery and batch application print forms
remain fail-closed.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`, `./scripts/check-contracts.sh`,
the frontend generated-catalog checks and `git diff --check`. Live
credentialed staging qualification remains a separate gate.
