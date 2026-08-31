# Task 183: MAX inbound webhook reception

## Status

`repository-complete` — 2026-08-31.

## Objective

Connect the already qualified MAX `social.webhooks` SDK receiver to the
production public webhook boundary so verified provider deliveries become
tenant-scoped, replay-safe EventBus/outbox events.

## Deliverables

- admit `social.webhooks` and its webhook-secret configuration in the built-in
  runtime-support contract;
- add a public, tenant-bound social webhook route for MAX;
- resolve accounts and capability snapshots with default-deny behavior;
- pass the ephemeral `X-Max-Bot-Api-Secret` to the adapter;
- persist only a minimized `commerce.social.webhook_received.v1` event through
  the Task-009 Inbox and transactional outbox;
- add the event schema, catalog fixture, generated projections, tests and
  architecture evidence.

## Scope limits

The task admits inbound receipt and durable replay protection only. MAX
subscription/unsubscription lifecycle, publication edit/delete, callback
actions, Long Polling and credentialed live provider qualification remain
outside the application route.

## Security and compatibility

The route always returns an empty 200 acknowledgement and never exposes account
existence or verification errors. The adapter verifies the separate secret,
configured channel, update type, MID and timestamp before claiming a delivery.
The raw provider body and verification token are not persisted in the event;
the host event contains only bounded normalized identifiers and a payload
fingerprint. The SocialWebhookDeduplicator interface becomes a typed claim
boundary so host persistence cannot lose the verified event metadata.

## Verification

Run the MAX adapter, runtime, API and event-contract tests, generated catalog
check, frontend shell validation, contract/SDK/migration checks, `go test
./...`, `go vet ./...` and `git diff --check`. Credentialed live MAX webhook
qualification remains a separate release gate.
