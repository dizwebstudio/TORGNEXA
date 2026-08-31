# Task 187: Почта России — перевод партии в архив

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Russian Post formed-batch archive gap with an approval-bound and
idempotent operation that preserves the provider's reversible archive
semantics instead of pretending that a separate cancellation endpoint exists.

## Deliverables

- add `logistics.batches.archive` to the provider-neutral SDK and Russian Post
  runtime admission;
- implement the official `PUT /1.0/archive` request with one numeric batch
  name and strict response matching;
- expose `POST /api/v1/logistics/batches/archive/{batch_id}` with capability,
  approval, tenant scope and operation-receipt replay protection;
- add the settings UI, generated SDK/catalog projections, deterministic
  transport/API/runtime tests and qualification evidence.

## Safety boundary

Only one numeric Russian Post batch name is sent to the fixed HTTPS endpoint.
The host accepts a result only when the single returned `batch-name` exactly
matches the requested batch and normalizes it to `ARCHIVED`. A pending receipt
blocks blind retry after an ambiguous provider result; restoring an archived
batch remains a separate, unqualified operation.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains a
deployment gate.
