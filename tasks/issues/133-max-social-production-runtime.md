# Task 133 — MAX Social production runtime

Status: Repository complete; live-provider gate pending

## Problem

Task 042 implemented and qualified the MAX SDK adapter, but the production API,
worker and frontend did not compose it. The runtime-truth contract therefore
had to keep MAX planned even though provider code existed.

## Scope

- Compose MAX through the existing provider-neutral Social Core publication
  API, leased worker and append-only receipt recovery path.
- Admit only `social.post.text` with the official 4000-code-point limit.
- Store the bot token only through SecretProvider and keep numeric `chat_id` in
  strict non-secret account configuration.
- Use the current `platform-api2.max.ru` authority, a raw bot token in the
  `Authorization` header and host-owned public-address/redirect-deny egress.
- Present Telegram and MAX as working providers on the dedicated Social surface
  without enabling generic product synchronization.

## Explicit exclusions

Media, video, buttons, status reads and webhooks remain SDK capability ceilings
until their upload, release, reconciliation and durable Inbox composition is
connected to production. VK remains planned until the host owns OAuth refresh
and normalizes stored OAuth bundles into callback-scoped access tokens.

## Acceptance criteria

- MAX account health and text publication resolve through the built-in registry.
- Only `POST /messages?chat_id=...` and the exact health reads can leave the host
  transport; media upload and all other paths fail closed.
- API text validation uses the selected account's generated runtime limit.
- Worker recovery reuses Task-132 receipts and never replays an ambiguous send.
- Runtime contract, generated Go/TypeScript catalogs, frontend, documentation
  and counts agree on 11 generic, 9 separate-surface and 18 planned entries.
- Go test/vet, contracts, architecture, frontend and production build gates pass.

## Live qualification

Repository completion is not a claim of remote delivery. A deployment must run
a non-production MAX bot and dedicated test channel qualification before it can
claim the `RUNTIME-133` live-provider gate.
