# Task 192: Telegram — редактирование опубликованного сообщения

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Telegram single-message edit gap with an approval-bound operation
that can update an existing remote message without changing its provider
identity or issuing a duplicate call on retry.

## Deliverables

- admit `social.post.edit` for the Telegram built-in runtime;
- expose `PATCH /api/v1/social/publications/{publication_id}` with strict
  request validation, tenant scope, capability checks, approval and
  idempotency receipts;
- resolve the editor only for Telegram and load its non-secret channel
  configuration through the existing secret/config boundary;
- accept only a confirmed update for the exact remote publication receipt;
- add OpenAPI and generated SDK projections, Social UI editing, deterministic
  API/runtime tests and qualification evidence.

## Safety boundary

The route accepts only a published local publication, an active healthy
Telegram account and its immutable remote receipt. It requires a matching
approved `social.publication.edit` / `social_publication` request with
write-sensitive risk, the enabled `social.post.edit` capability and a
tenant-scoped `Idempotency-Key`. Completed replays return the stored normalized
result; pending or ambiguous outcomes do not issue another provider call. The
adapter result must confirm `published`, `updated=true` and the exact same
remote publication ID. Provider payloads, credentials and raw channel
configuration do not enter the operation receipt.

## Verification

Run connector, API, runtime-admission, OpenAPI parity, generated SDK,
frontend, contract, migration, architecture, `go test ./...`, `go vet ./...`
and `git diff --check` checks. Credentialed live qualification remains a
deployment gate and must use a disposable Telegram test message. Deletion and
webhook operations remain outside this admission.
