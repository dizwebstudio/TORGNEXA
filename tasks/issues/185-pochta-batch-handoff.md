# Task 185: Почта России — передача партии в работу

## Status

`repository-complete` — 2026-08-31.

## Objective

Connect the existing Russian Post batch projection to the official check-in
operation so an approved, already formed batch can be handed to postal
processing without exposing provider-specific transport details to the host.

## Deliverables

- add the provider-neutral `logistics.batches.submit` capability and SDK port;
- implement the Russian Post `POST /1.0/batch/{batch-name}/checkin` adapter call;
- expose an authenticated, tenant-scoped and approval-bound submit route;
- use the existing operation receipt for idempotent replay and ambiguous
  outcomes;
- add the generated SDK/catalog projections, UI control, deterministic
  transport tests and qualification evidence.

## Safety boundary

The route accepts only a bounded batch reference and the optional documented
online-balance flag. The adapter sends no request body, requires the exact
batch reference in the normalized result and accepts only a successful
`f103-sent` response. Approval and `Idempotency-Key` are independent gates;
replays never issue a second provider request, while an ambiguous transport
outcome remains pending until reconciliation.

## Verification

Run the connector, transport, API, runtime admission, OpenAPI parity,
generated SDK, frontend, contract, migration, architecture, `go test ./...`,
`go vet ./...` and `git diff --check` checks. Credentialed live Russian Post
qualification remains a deployment gate.
