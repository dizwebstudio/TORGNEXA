# Task 189: Почта России — чтение партий из архива

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Russian Post archived-batch directory gap with a bounded read-only
operation that does not expose the provider's order rows or raw response.

## Deliverables

- add `logistics.batches.archive.read` to the provider-neutral capability and
  Russian Post runtime admission;
- implement the official `GET /1.0/archive` request with fixed HTTPS egress
  and strict normalized response validation;
- expose `GET /api/v1/logistics/batches/archive` with authenticated tenant
  scope and a host limit of 100 records;
- add the settings UI, generated SDK/catalog projections, deterministic
  transport/API/runtime tests and qualification evidence.

## Safety boundary

The archive endpoint is read-only and accepts no caller-supplied provider URL.
The host rejects malformed, duplicate or over-limit rows and returns only the
batch reference, status, shipment count and observation time. Provider order
contents and raw fields stay behind the connector boundary.

## Verification

Run connector, transport, API, runtime admission, OpenAPI parity, generated
SDK, frontend, contract, migration, architecture, `go test ./...`, `go vet
./...` and `git diff --check` checks. Credentialed live qualification remains
a deployment gate.
