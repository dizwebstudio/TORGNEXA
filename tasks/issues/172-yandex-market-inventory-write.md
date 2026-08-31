# Task 172: Yandex Market inventory write

## Status

`repository-complete` — 2026-08-31.

## Objective

Expose Yandex Market's documented stock-update APIs through the existing
provider-neutral `InventoryWriter` and commerce-sync worker, without guessing
warehouse semantics or treating asynchronous provider acceptance as
reconciliation.

## Deliverables

- manifest and generated runtime-support admission for `inventory.write`;
- explicit partner-warehouse `POST` and grouped-warehouse `PUT` request mapping;
- numeric warehouse scope, quantity bound and response validation;
- built-in runtime registry admission and generic worker/API bridge reuse;
- deterministic success, mode, bounds, warehouse and provider-failure tests;
- synchronized connector docs, integration matrix, task record and architecture
  review.

## Scope limits

Only one SKU and one exact non-negative integer available quantity are sent per
operation. Partner mode sends `partnerWarehouseId`; grouped mode validates the
host-configured warehouse but follows the provider endpoint shape without
inventing a warehouse field. Provider product, order-status, campaign and
notification-setting writes remain fail-closed. The provider's asynchronous
`OK` response is an acceptance receipt, not proof of remote convergence.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh` and the frontend generated-catalog checks. Live
credentialed staging qualification remains separate from this deterministic
repository qualification.
