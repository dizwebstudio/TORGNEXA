# Task 186: Почта России — отдельная возвратная отправка

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the remaining Russian Post standalone-return gap with a bounded,
approval-bound and idempotent operation that does not require an existing
carrier RPO.

## Deliverables

- add `logistics.return.separate.create` to the provider-neutral SDK and
  Russian Post runtime admission;
- implement `PUT /1.0/returns/return-without-direct` with exactly one request
  item and strict `position=0`/`return-barcode` response validation;
- expose `POST /api/v1/logistics/returns/separate` with capability, approval,
  tenant scope and operation-receipt replay protection;
- keep addresses and names request-scoped and out of durable receipts;
- add the settings UI, generated SDK/catalog projections, deterministic
  transport/API/runtime tests and qualification evidence.

## Safety boundary

The host accepts only bounded Russian addresses, names, mail type, optional
order/post-office references and declared value in minor units. The provider
adapter sends one item, converts value to whole roubles, and returns only a
normalized shipment reference/status/tracking number. A pending receipt blocks
blind retry after an ambiguous provider result.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains a
deployment gate.
