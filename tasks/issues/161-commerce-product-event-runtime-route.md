# Task 161: Canonical product event runtime route

## Status

`repository-complete` — 2026-08-29.

## Objective

Route `commerce.catalog.product_changed.v1` through the production
`commerce-sync` worker for connectors whose generated runtime support admits
outbound `products` synchronization.

## Deliverables

- strict product event validation and canonical product snapshot loading;
- tenant-scoped `product` mapping resolution with safe create-after-receipt;
- provider-native lifecycle status translation at the built-in registry;
- ProductWriter invocation with stable policy/event idempotency keys;
- existing receipt, retry/DLQ and capability gates retained;
- worker tests, ADR and synchronization documentation.

## Scope limits

`commerce.catalog.offer_changed.v1` remains ignored. Price and inventory
events continue to require an existing `offer` mapping. Product payloads keep
their existing identity/version/status/change contract; descriptions and
other mutable fields are read from the tenant-scoped canonical product.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`, and
`./scripts/check-contracts.sh`. For end-to-end validation, enable an outbound
`products` policy, the account `products.write` capability, and publish a
canonical product event against a connector with a qualified ProductWriter.
