# ADR-0103 — MAX Social production runtime

Status: Accepted

## Context

Task 042 qualified the MAX SDK adapter and Task 020 supplied canonical Social
Core persistence. Task 132 then introduced the provider-neutral production API,
leased delivery worker and append-only remote receipts for text publication,
but only Telegram was composed. MAX therefore remained truthfully planned.

## Decision

Compose the existing MAX adapter only at the built-in provider boundary and
expose `social.post.text` on the dedicated `social` surface. API, canonical
state and worker remain provider-neutral. Account configuration contains one
non-zero numeric `chat_id`; bot tokens stay callback-scoped in SecretProvider.

The host permits only the exact MAX health reads and
`POST /messages?chat_id=...` at the current `platform-api2.max.ru` authority.
It supplies the raw bot token in `Authorization`, denies redirects and private
address targets, and does not admit media upload. The selected provider's text
ceiling is generated from the runtime-support contract and enforced before any
canonical publication is created.

Task-132 append-only receipts and ambiguous-write recovery are reused. An
existing receipt permits finalization after a reclaimed lease; absence of a
receipt while canonical state is `publishing` fails as `write_outcome_unknown`
without another remote send.

## Consequences

- MAX becomes a real provider on the Social surface, not a product-sync card.
- The production subset is deliberately smaller than Task-042 SDK coverage:
  media, buttons, status reads and webhooks remain unavailable end to end.
- Social UI is provider-neutral and applies the exact 4000/4096 MAX/Telegram
  limits generated from one runtime contract.
- Rollback stops new dispatch without deleting canonical state or receipts;
  already published remote messages are not reversed.

## Compatibility impact

The runtime-support contract additively admits MAX and adds an optional bounded
social-text-limit field. OpenAPI operations, generated public SDK methods,
Connector SDK v1 and event payloads are unchanged.

## Migration and data impact

No migration or backfill is required. MAX publications use the existing
Task-020 Social Core tables and migration-17 append-only receipt/lease model.

## Security and privacy impact

Publication text remains tenant content under forced RLS and is excluded from
events and receipts. Bot tokens are never stored in runtime configuration,
events or logs. Egress has a fixed authority, public DNS/IP validation and
redirect denial; raw provider responses are not logged. Unsupported media and
webhook operations fail closed.

## Operational impact

Operators configure a MAX connector account with bot token plus `chat_id`, run
health, enable `social.post.text`, activate the account, then bind it as a
channel under `/social`. Repository qualification cannot replace a live test
with a non-production bot and dedicated channel.

## Alternatives considered

Advertising every Task-042 manifest capability was rejected because the host
does not yet compose upload, media release, status reconciliation or durable
webhook Inbox flows. Provider-specific API routes and worker branches were
rejected because Social Core already provides a capability-based boundary.
Automatic retry after an ambiguous send was rejected because MAX exposes no
caller idempotency key for this operation. Keeping MAX planned despite the
completed text bridge was rejected because it would make the catalog false.
