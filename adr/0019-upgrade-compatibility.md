# ADR 0019: Upgrade compatibility

Status: Accepted

## Context

TORGNEXA must support rolling application deployment, interrupted maintenance,
tenant isolation, and forward recovery without silently rewriting already
published database, API, event, or plugin contracts. A SQL filename alone does
not prove order, compatibility, integrity, or whether a long backfill finished.

## Decision

Database changes use an immutable checksum catalog and explicit
expand/migrate/contract metadata. Existing migrations `000001`–`000003` are a
one-time framework bootstrap; subsequent migrations record their own catalog
metadata in `migration_history` inside the same SQL transaction using
runner-supplied, non-secret session settings. Checksum drift, history gaps, and
an applied version unknown to the running binary fail closed; automatic
downgrade or history repair is forbidden.

Expand preserves old readers/writers and permits the new binary on the old
schema. Migrate keeps rolling compatibility while a bounded, retry-idempotent
backfill advances an opaque stable cursor. Contract may remove the old shape
only after named completion/traffic/version preconditions, a verified backup,
and a separately reviewed release.

Backfills use persistent checkpoints, expiring leases, and monotonic fencing
generations. A scheduler tick processes at most one bounded batch. A crash may
repeat the last uncommitted batch, so processors are idempotent and externally
visible side effects require their normal outbox/inbox/idempotency boundary.
Raw row content and errors never enter cursors or failure metadata.

Public API, event, webhook, and plugin changes continue to follow their own
versioned compatibility policies; database compatibility metadata cannot waive
those contracts. Release qualification includes old-to-new and fresh-install
schema parity, interrupted/resumed backfill, tenant isolation, and the verified
backup/restore gate.

## Consequences

- Every SQL migration and its metadata are reviewed together; modifying an
  applied file is detectable and prohibited.
- Destructive SQL is restricted to an explicit contract phase and cannot share
  an expand/migrate change.
- Global migrations/backfills use a separate reviewed operational identity;
  tenant jobs carry both organization and workspace scope.
- Rollback normally means stopping the new binary, repairing forward, or
  restoring the verified checkpoint. It does not mean running ad-hoc down SQL.
- Provider-specific migration engines remain outside Core unless another ADR
  establishes a generic requirement.
