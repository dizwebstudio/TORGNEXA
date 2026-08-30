# Task 160: PrestaShop commerce sync runtime route

## Status

`repository-complete` — 2026-08-29.

## Objective

Make PrestaShop's qualified price, StockAvailable and order-state operations
available through the production worker rather than exposing SDK-only
capabilities in the integration catalog.

## Deliverables

- dedicated `torgnexa.commerce-sync.v1` Kafka consumer group;
- outbound `prices`, `inventory` and `orders` policy admission for PrestaShop;
- tenant-scoped `offer` mapping resolution and account capability re-check;
- tenant-configured order-state mapping for the canonical order lifecycle;
- deterministic remote idempotency keys and durable `sync_local_receipts`;
- retry/DLQ classification for transient and permanent failures;
- generated runtime catalog, UI entity label, documentation and architecture
  review synchronized.

## Scope limits

Only regular prices and non-negative discrete stock quantities are routed.
Price reads, compare-at/cost prices, fractional units and multi-warehouse
aggregation remain outside this route. Order reads and state transitions use
the native `orders`/`order_details`/`order_histories` resources and require all
five canonical states to be explicitly mapped to unique PrestaShop state IDs.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, and the PrestaShop Docker Webservice smoke.
For end-to-end worker validation, create enabled `prices`, `inventory` and
`orders` policies, enable the corresponding account capabilities, add an
`offer` mapping and configure `order_statuses` before publishing canonical
domain events. The official Webservice smoke remains the provider/API gate;
the worker route additionally requires a running TORGNEXA Compose stack.
