# ADR-0135 — Приём входящих webhook MAX

Status: Accepted

## Context

The MAX adapter already verifies production Webhook updates, normalizes the
configured channel and update type, and derives a content-addressed delivery
identity. The built-in runtime and application route did not expose that
capability, leaving valid provider deliveries fail-closed before the host's
durable replay boundary.

## Decision

Admit `social.webhooks` for MAX in the built-in runtime-support contract and
register a public route under `/api/v1/webhooks/social/`. The route resolves
the tenant and active connector account from the URL, requires the account's
enabled capability, passes the ephemeral MAX secret header to the adapter and
uses a typed host-owned claim. Verified claims are recorded as the minimized
`commerce.social.webhook_received.v1` event through the tenant-scoped Task-009
Inbox and transactional outbox.

The event contains connector account, delivery, normalized event/channel/object
identities and the provider fingerprint. It deliberately excludes raw
provider JSON, secrets and unreviewed PII. Subscription lifecycle remains an
SDK-only surface until it has a separate application authorization and
configuration contract.

## Alternatives considered

Доверять входному событию без повторной проверки было отклонено из-за риска
подделки, replay и cross-tenant routing.

## Consequences

MAX deliveries попадают в общий durable webhook boundary после проверки и
deduplication; subscription lifecycle остаётся отдельным SDK-only scope.

## Migration and data impact

Миграция не требуется: используются существующие Task-009 inbox и
transactional outbox records для verified delivery evidence.

## Security and privacy impact

The route is unauthenticated by design but always returns `{}` with HTTP 200,
and the fixed URL shape plus capability check prevents cross-tenant routing.
The provider adapter remains responsible for constant-time secret comparison
and exact MAX payload validation. The additive typed claim SDK change requires
all host deduplicators to validate bounded fields before persistence; existing
connector behavior remains compatible through the adapter's unchanged public
receiver method.

## Operational impact

Inbox receipt insertion and outbox publication commit atomically, making
provider retries idempotent and allowing Kafka publication to follow the
authoritative database record. Operators must configure a separate webhook
secret reference for MAX and keep the provider endpoint on HTTPS. Live MAX
credentials, endpoint delivery and provider qualification remain deployment
gates.

## Compatibility impact

The public webhook route keeps its existing tenant/account URL shape and
acknowledgement behavior. MAX is admitted through the provider-neutral webhook
contract without changing the event envelope compatibility.
