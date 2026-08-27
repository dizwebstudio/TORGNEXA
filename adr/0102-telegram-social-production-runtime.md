# ADR-0102 — Telegram Social production runtime

Status: Accepted

## Context

Task 041 qualified the Telegram SDK adapter and Task 020 supplied canonical
Social Core persistence, but production API, worker and frontend composition
did not connect them. The truthful runtime catalog therefore kept Telegram
planned even though provider code existed.

## Decision

Compose the existing Telegram adapter only at the built-in provider boundary
and expose `social.post.text` on a dedicated `social` surface. API and worker
remain provider-neutral and operate on Task-020 Content, ContentVariant,
ChannelAccount and Publication records.

Remote publication identifiers remain outside Social Core in an append-only,
tenant-scoped receipt table. A worker lease may be reclaimed while canonical
state is `publishing`: an existing receipt permits safe finalization to
`published`; no receipt means the remote outcome is unknown and the record is
failed without another send. This is required because Telegram send methods do
not accept TORGNEXA's idempotency identity.

The generic connector-account control plane may configure a connector on the
dedicated social surface, but generic product sync remains unavailable. Runtime
capability admission is an exact manifest subset and initially contains only
text publication.

## Consequences

- Telegram becomes a real, separately surfaced integration rather than a
  product-sync card.
- Bot tokens stay in SecretProvider callbacks; `chat_id` is non-secret runtime
  configuration.
- Media and remote mutation capabilities remain SDK ceilings, not production
  claims, until upload, approval and reconciliation flows are composed.
- Rollback may stop API/worker/frontend code without deleting canonical state or
  append-only receipts. Already published Telegram messages are not rolled back.

## Compatibility impact

Five authenticated REST operations are additive in OpenAPI 0.21.1 and all
generated SDKs. Connector SDK v1 and existing event payloads are unchanged.
The runtime-support surface enum additively gains `social`.

## Migration and data impact

Migration 17 adds one append-only tenant receipt table and one worker job kind.
There is no backfill: existing Social Core records keep their meaning. Old
binaries ignore the new rows; a new worker on schema 16 treats the unavailable
job kind as rolling-upgrade schema absence and continues its other components.

## Security and privacy impact

Publication text is tenant content and stays in forced-RLS Social Core tables.
It is excluded from events and remote receipts. Bot tokens remain encrypted and
exist in plaintext only within a SecretProvider callback. The Telegram host is
fixed, DNS targets must be public, redirects are denied and raw provider
responses/errors are never logged.

## Operational impact

Operators configure Telegram in the integration drawer and publish in
`/social`. Worker status codes expose bounded failure reasons. A missing receipt
after reclaiming `publishing` requires operator review; it never causes an
automatic resend. Deploy migration 17 before relying on delivery, although the
new worker remains alive during an expand rollout against schema 16.

## Alternatives considered

Marking Telegram ready in generic product synchronization was rejected because
social publication is not product reconciliation. Synchronous provider writes
from the API were rejected because request lifetime and retries would bypass the
durable worker lifecycle. Storing the remote ID on canonical Publication was
rejected because provider identity is adapter evidence. Automatically retrying
an unconfirmed send was rejected because Telegram has no caller idempotency key
and could create duplicate public posts.
