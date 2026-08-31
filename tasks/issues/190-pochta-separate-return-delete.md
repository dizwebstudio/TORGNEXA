# Task 190: Почта России — удаление отдельной возвратной отправки

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Russian Post standalone-return deletion gap with a narrow,
approval-bound operation that never treats a provider error response as a
successful deletion.

## Deliverables

- add `logistics.return.separate.delete` to the provider-neutral capability and
  Russian Post runtime admission;
- implement the official `DELETE /1.0/returns/delete-separate-return` request
  with fixed HTTPS egress and strict empty-success/error-code handling;
- expose `DELETE /api/v1/logistics/returns/separate/{return_id}` with
  authenticated tenant scope, approval and idempotency receipts;
- add the generated OpenAPI/SDK/catalog projection, settings action,
  deterministic transport/API/runtime/connector tests and qualification
  evidence.

## Safety boundary

The route accepts only a validated provider barcode and connector account ID.
It requires an approved `fulfillment.return.separate.delete` request and
tenant-scoped `Idempotency-Key`; completed replays do not issue another
provider call. The adapter accepts only a `2xx` response with an empty body or
an empty JSON `code`; provider error codes remain failures. Only the barcode,
`DELETED` status, boolean acknowledgement and observation time cross the
connector boundary.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains
a deployment gate and must use a disposable test return.
