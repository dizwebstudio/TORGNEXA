# Task 193: Telegram — удаление опубликованного сообщения

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Telegram single-message delete gap with an approval-bound operation
that removes only the exact remote message recorded for a local publication
and cannot repeat a privileged provider call after a retry.

## Deliverables

- admit `social.post.delete` for the Telegram built-in runtime;
- expose `DELETE /api/v1/social/publications/{publication_id}` with tenant
  scope, strict capability checks, approval and idempotency receipts;
- resolve the deleter only for Telegram and load its channel configuration
  through the existing secret/config boundary;
- accept only a confirmed deletion for the exact immutable remote receipt;
- add OpenAPI and generated SDK projections, Social UI confirmation, and
  deterministic API/runtime/connector tests and qualification evidence.

## Safety boundary

The route accepts only a published local publication, an active healthy
Telegram account and its immutable remote receipt. It requires a matching
approved `social.publication.delete` / `social_publication` request with
write-sensitive risk, the enabled `social.post.delete` capability and a
tenant-scoped `Idempotency-Key`. Completed replays return the stored normalized
result; pending or ambiguous outcomes do not issue another provider call. The
adapter result must confirm `deleted=true` and the exact same remote
publication ID. Provider payloads, credentials and raw channel configuration
do not enter the operation receipt.

## Verification

Run connector, API, runtime-admission, OpenAPI parity, generated SDK,
frontend, contract, migration, architecture, `go test ./...`, `go vet ./...`
and `git diff --check` checks. Credentialed live qualification remains a
deployment gate and must use a disposable Telegram test message because
deletion is irreversible. Webhooks remain outside this admission.
