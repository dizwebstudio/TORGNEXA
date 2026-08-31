# Task 181: Telegram HTTPS publication buttons

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the runtime gap for Telegram `social.post.buttons` while keeping the
provider-neutral social publication contract safe and bounded.

## Deliverables

- typed HTTPS link-button value in Social Core;
- immutable PostgreSQL variant snapshot with tenant/RLS and database shape
  validation;
- Social API input/output and idempotency comparison for up to eight buttons;
- leased worker mapping plus account/channel capability checks;
- `/social` form with validation, add/remove controls and history rendering;
- Telegram runtime support admission and generated catalog/SDK updates;
- deterministic Core/worker/connector tests, migration checks and synchronized
  documentation/evidence.

## Scope limits

Only HTTPS URL buttons are admitted. Callback-data buttons, inbound updates,
edit/delete and provider-specific action state remain fail-closed. Telegram
albums cannot carry buttons because the Bot API does not provide the same
per-publication markup surface for `sendMediaGroup`; the adapter rejects that
combination before egress.

## Security and compatibility

The button snapshot contains only user-visible text and HTTPS URLs. No
callback payload, secret, provider response or raw request body is persisted.
The existing publication approval/capability, tenant, worker lease, receipt
recovery and ambiguous-write policies remain unchanged. The migration is
additive with a default empty array so old variants remain readable.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, migration/static checks, generated SDK checks,
frontend shell validation and `git diff --check`. Credentialed live Telegram
qualification remains a separate release gate.
