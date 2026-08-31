# Task 184: Почта России — формирование партии

## Status

`repository-complete` — 2026-08-31.

## Objective

Connect the existing Russian Post batch API contract to a guarded production
route so already-created backlog orders can be formed into one remote batch.

## Deliverables

- add the provider-neutral `logistics.batches.create` SDK capability;
- validate and bound the request to 1–100 unique order references;
- implement the official `POST /1.0/user/shipment` adapter call;
- expose `POST /api/v1/logistics/batches` with tenant, capability, approval and
  idempotency gates;
- persist only the normalized batch result in the existing operation receipt;
- add the generated SDK/catalog projections, UI control, tests and evidence.

## Safety boundary

The route accepts only numeric Russian Post backlog order IDs at the adapter
boundary and never projects the order list or raw provider response into the
host. Replays with the same idempotency key and digest do not call the provider
again. A transport outcome that may have reached the provider leaves the
receipt pending until reconciliation. Handoff to postal processing and a
separate return shipment remain fail-closed.

## Verification

Run the connector, transport, API, repository, contract, generated SDK,
frontend shell, migration, architecture, `go test ./...`, `go vet ./...` and
`git diff --check` checks. Credentialed live Russian Post qualification remains
a deployment gate.
