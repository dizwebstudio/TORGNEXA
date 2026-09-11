# ADR-0191 — Atomic connector account mutations and audit

Status: Accepted

## Context

Connector account writes committed before authoritative audit. Credential
enrollment could return 500 after binding new material and revoking the previous
reference. Compensating revocation could neither restore the previous binding
nor provide authoritative evidence. Bootstrap mutations had the same gap.

## Decision

Extend ADR-0184's synchronous tenant/pool-bound PostgreSQL transaction to account
creation, disable/enable, capability revisions, persisted health and history,
credential enrollment, OAuth lifecycle, bootstrap previews/jobs and schedules.
Connector and sync repositories join that transaction and leave commit to its
owner. Pool or tenant changes fail closed. Success is returned only after commit.

Credential creation, binding, old reference revocation and audit share one
transaction. Any statement, cancellation or commit failure rolls back all four.
The optional `secrets.TransactionalProvider` contract must join the caller's
authoritative transaction; an unsupported provider cannot perform these writes.
The Community encrypted provider delegates to its PostgreSQL repository.

OAuth has two explicit local commit boundaries around the remote side effect:

1. Start commits encrypted PKCE material, pending state and `oauth_started`
   audit. A replay rolls back its unused candidate material and returns the
   original pending authorization URL without another audit record.
2. Callback atomically consumes state, checks actor/callback/expiry/account
   version, reads and revokes temporary material, and appends
   `connector.account.oauth_callback_claimed` before exchanging the code.
3. Following the remote exchange, encrypted bundle creation, binding, old
   reference revocation and `oauth_completed` audit commit together. Provider
   failure instead commits normalized health/history and `oauth_failed` audit.

Once a callback claim commits, no later failure reopens its one-time state. A
failure after exchange preserves the previous account binding but requires a
fresh OAuth start with a new key. PostgreSQL cannot roll back provider-issued
tokens. Remote health probes also run outside the account mutation transaction;
the host refresh behavior and limitations in ADR-0104 remain applicable.

Preview/job replays skip duplicate audit. Version-guarded account and schedule
writes retain their existing stale-version conflict behavior. Bootstrap database
timestamps are normalized to UTC before domain validation, as other repository
projections already do.

## Compatibility, security and privacy

No migration, new persisted fields, event or SDK payload changes are required.
OpenAPI documents atomic failures and callback retry semantics. Update API
instances together; an old binary still has the post-commit audit gap. Existing
missing historical evidence cannot be reconstructed by this change.

Audit keeps only connector/status/version and bounded operational counts/IDs.
Actor subjects use the existing pseudonymous `actor.` projection. No secret
reference, state, code, token or provider body is included. The former
`has_secret_reference` summary key is removed because the database's existing
redaction constraint rejects credential-bearing keys even for boolean values.
Encrypted material and audit keep their existing retention policies.

## Validation and remaining scope

Real PostgreSQL with forced RLS, an unprivileged application role and a one-slot
pool verifies audit/revoke/commit/cancellation rollback, retries, concurrent
replacement, history, bootstrap consumption and OAuth's remote-call boundary.
Provider responses are deterministic fixtures. Full checks and evidence are in
`docs/audits/2026-09-10-connector-audit-fix.md`.

This closes the identified connector account/bootstrap mutation gap. Manual
sync/reconciliation dispatch, worker refresh and the remaining security settings
inventory are completed by ADR-0193, which closes Task 234.3 without changing
the still-open parts of Task 234.
