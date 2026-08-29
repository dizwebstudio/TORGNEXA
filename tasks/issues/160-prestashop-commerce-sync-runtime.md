# Task 160: PrestaShop commerce sync runtime route

## Status

`repository-complete` — 2026-08-29.

## Objective

Make PrestaShop's qualified price and StockAvailable writes available through
the production worker rather than exposing SDK-only capabilities in the
integration catalog.

## Deliverables

- dedicated `torgnexa.commerce-sync.v1` Kafka consumer group;
- outbound `prices` and `inventory` policy admission for PrestaShop;
- tenant-scoped `offer` mapping resolution and account capability re-check;
- deterministic remote idempotency keys and durable `sync_local_receipts`;
- retry/DLQ classification for transient and permanent failures;
- generated runtime catalog, UI entity label, documentation and architecture
  review synchronized.

## Scope limits

Only regular prices and non-negative discrete stock quantities are routed.
Price reads, compare-at/cost prices, fractional units, order status and
multi-warehouse aggregation remain outside this route until their
provider-neutral semantics are separately qualified.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, and the PrestaShop Docker Webservice smoke.
For end-to-end worker validation, create enabled outbound `prices` and
`inventory` policies, enable the corresponding account capabilities and add an
`offer` mapping before publishing a canonical domain event.
