# ADR-0193 — Atomic privileged dispatch and OAuth refresh intent

Status: Accepted

## Context

ADR-0184 and ADR-0191 made the main Settings and connector-account mutations
atomic with authoritative audit. The remaining Task 234.3 inventory found four
gaps: manual reconciliation runs committed before audit, MCP and AI account
handlers appended a second audit after their governed mutation, MCP agent policy
and kill-switch audit was best effort, and an OAuth refresh could change state at
the provider before any durable local evidence existed.

PostgreSQL cannot roll back a remote OAuth token exchange. The required boundary
therefore differs from a local settings mutation: durable intent must exist
before the remote effect, while local ciphertext rotation remains protected by
the existing transaction advisory lock from ADR-0104.

## Decision

Manual policy run, connector-account sync fan-out and reconciliation-job
creation use deterministic run identifiers derived from tenant scope,
idempotency key and policy. Run rows and one bounded `audit_records` entry commit
in the same tenant/pool-bound PostgreSQL transaction. An exact retry returns the
existing run and appends no audit; reuse for different input fails closed.

MCP client account create/disable/rotate and AI provider account create/disable
join that same audit transaction. Their existing operation receipt and
`security_evidence` remain authoritative, and the Settings audit row now commits
with them. AI create deliberately rolls back the unused candidate secret on an
exact replay, so a retry leaves one account, one active secret reference, one
receipt and one copy of each evidence record.

MCP agent policy installation and the tenant kill switch now require an
`Idempotency-Key`. Their immutable revision, operation receipt and Settings audit
commit together. A retry returns the original revision. Repository reads and
writes reject a transaction belonging to a different database pool or tenant.

Before an authorization-code refresh calls the provider, the coordinator
inserts deterministic `connector.oauth_refresh.requested` evidence. The identity
is derived from tenant, account, connector, opaque secret reference and current
secret version, but the stored evidence contains only account ID, connector ID
and numeric version. Failure to persist it returns
`oauth_refresh_unavailable` and the provider is not called. Concurrent callers
for the same version converge on one row; the advisory lock and locked re-read
still ensure at most one provider refresh and one ciphertext rotation.

## Compatibility, security and privacy

No migration is required: the implementation reuses `operation_receipts`,
`audit_records` and `security_evidence`. Deploy API and workers together because
the refresh coordinator interface changed. Clients of MCP policy installation
and kill-switch mutation must send the now-required idempotency header.

Audit summaries contain only pseudonymous actor references, stable internal
resource IDs, status/version, connector ID and bounded counts. They exclude
email, raw OIDC subject, secret reference, credentials, token endpoint payloads
and provider responses. The refresh evidence proves admission of the remote
attempt; it does not claim the remote exchange succeeded. A later provider or
rotation failure remains observable through the existing normalized connector
health state.

## Validation

The disposable PostgreSQL gate uses an unprivileged role under forced RLS and
injects audit/evidence failures after business statements. It verifies rollback
and exact replay for manual reconciliation, account sync, MCP and AI accounts,
MCP agent policy and kill switch. A separate refresh trigger proves that the
provider is not called without durable intent, then verifies one minimized
evidence row and one successful rotation after retry. The existing one-slot and
bounded-pool ADR-0104 concurrency tests continue to pass.

Validation evidence is recorded in
`docs/audits/2026-09-11-privileged-audit-completion.md`.
