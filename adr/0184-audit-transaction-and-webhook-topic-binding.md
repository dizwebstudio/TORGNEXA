# ADR-0184 — Atomic settings audit and trusted commerce webhook topics

Status: Accepted

## Context

Audit findings A05–A08 exposed gaps between the intended contracts and their
PostgreSQL/runtime composition: settings committed before audit, OAuth refresh
opened nested transactions on the same bounded pool, commerce webhook topic
expectations came from the incoming header, and ambiguous payment creation
could not be reconciled by its original merchant identifier.

## Decision

Settings mutations and authoritative audit share a tenant-scoped PostgreSQL unit
of work. Its context is internal, synchronous and bound to the exact pool and
tenant; participating repositories reject scope/pool changes and leave commit
and rollback to the outer owner. The API sends success only after that commit.
Idempotent mutation results skip duplicate audit records. This covers member
invitation/role/status/profile, current-user profile/avatar removal, workspace,
managed identity providers and connector runtime configuration. Other settings
surfaces remain in the broader Task 234.3 inventory.

OAuth refresh uses the same transaction primitive: advisory lock, re-read and
encrypted rotation use one connection. Try-lock losers release their transaction
before waiting. The remote request retains its existing timeout and rotating
refresh tokens remain serialized across API and worker processes.

Each commerce webhook callback carries a separate random 256-bit `subscription`
query reference. Only its SHA-256 and exact canonical topic are stored in
`commerce_webhook_subscriptions` inside the existing tenant/account runtime
configuration. The host resolves the reference and compares the normalized
provider header before verification/Inbox claim. Provider signatures and
Saleor's signed body event check remain mandatory. Missing or revoked bindings
fail closed with the existing uniform acknowledgement. References, signatures
and raw payloads never enter logs, audit or outbox.

Only the request that inserts a payment intent may dispatch its remote create.
A transport error leaves `pending`; client retries do not dispatch again.
Authenticated reconciliation may bind an unbound payment through optional SDK
`PaymentSettlement.ExternalID`, provided account, amount and currency match.
YooKassa projects its echoed `metadata.external_id` into this field. Binding
first commits `pending -> created`; the next transition applies the observed
state. If that second commit fails, a subsequent cycle resumes by remote ID.
Payment audit IDs use the required UUIDv7 format. Lifecycle enums are unchanged.

## Compatibility, migration and privacy

No database migration or backfill is needed. PostgreSQL remains the operational
source of truth. REST response shapes and public events remain unchanged; the
SDK settlement field is optional and contains only the original merchant ID.

The required callback reference is a security tightening of the existing v1
webhook operation. Deploy it in a documented maintenance window: pause provider
callbacks, upgrade API/worker, configure per-topic digests and matching provider
URLs, then resume and reconcile the paused interval. There is no grace period
that trusts unsigned topic headers. Reverting the binary without also pausing
callbacks would reintroduce the vulnerable boundary. See the provisioning steps
in `docs/audits/2026-09-09-a05-a08-fixes.md`.

The reference is a callback credential; its digest and topic are non-PII
configuration metadata. Remove the binding to revoke it; rotation replaces the
digest and provider URL. They follow the existing connector configuration
lifecycle. No new personal fields or retention stores are introduced. Configure
reverse-proxy access logs to omit callback query strings as well.

## Validation and limits

`scripts/check-audit-postgres.sh` applies the complete migration catalog to an
isolated PostgreSQL, uses a non-superuser/non-BYPASSRLS application role and runs
failure, replay and concurrency scenarios with the race detector. Provider calls
use deterministic local fixtures; no real payment or external identity is used.

Ambiguous intents without matching provider observations stay pending for
operator reconciliation. The existing bounded reconciliation window/list
pagination is not a guarantee of unlimited historical recovery. This change
does not authorize a new create attempt after provider idempotency expiry.
