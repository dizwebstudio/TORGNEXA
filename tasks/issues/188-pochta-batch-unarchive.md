# Task 188: Почта России — возврат партии из архива

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Russian Post formed-batch archive-restore gap with a separate,
approval-bound and idempotent operation that preserves the provider's
reversible archive semantics.

## Deliverables

- add `logistics.batches.unarchive` to the provider-neutral SDK and Russian
  Post runtime admission;
- implement the official `POST /1.0/archive/revert` request with one numeric
  batch name and strict response matching;
- expose `POST /api/v1/logistics/batches/archive/revert/{batch_id}` with
  capability, approval, tenant scope and operation-receipt replay protection;
- add the settings UI, generated SDK/catalog projections, deterministic
  transport/API/runtime tests and qualification evidence.

## Safety boundary

Only one numeric Russian Post batch name is sent to the fixed HTTPS endpoint.
The host accepts a result only when the single returned `batch-name` exactly
matches the requested batch and normalizes it to `RESTORED` with
`archived=false`. A pending receipt blocks blind retry after an ambiguous
provider result; other archive operations remain fail-closed.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains
a deployment gate.
