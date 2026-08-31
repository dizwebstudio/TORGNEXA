# Task 191: Почта России — редактирование отдельной возвратной отправки

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Russian Post standalone-return edit gap with an approval-bound
operation that accepts only a provider response confirming the same return
barcode.

## Deliverables

- add `logistics.return.separate.edit` to the provider-neutral capability and
  Russian Post runtime admission;
- implement the official `POST /1.0/returns/{barcode}` request with fixed
  HTTPS egress and strict response confirmation;
- expose `POST /api/v1/logistics/returns/separate/{return_id}` with
  authenticated tenant scope, approval and idempotency receipts;
- add the generated OpenAPI/SDK/catalog projection, settings action,
  deterministic transport/API/runtime/connector tests and qualification
  evidence.

## Safety boundary

The route accepts only a validated provider barcode and connector account ID.
It requires an approved `fulfillment.return.separate.edit` request and a
tenant-scoped `Idempotency-Key`; completed replays do not issue another
provider call. The adapter accepts only a `2xx` response containing the same
validated `return-barcode` in one object or one response-array item. Empty,
malformed, error-bearing or mismatched responses remain failures. Addresses
and names are request-scoped and never enter the operation receipt.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains
a deployment gate and must use a disposable test return.
