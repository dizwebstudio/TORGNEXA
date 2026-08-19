# Task 112: Retire legacy inbox_events

## Status
`repository-complete`

## Objective
Remove the unused pre-Task-009 `inbox_events` compatibility table after fleet qualification, while preserving the canonical tenant-scoped `inbox_receipts` consumer idempotency contract.

## Dependencies
009, 027, 080

## Acceptance
- runtime and deployment checks use only `inbox_receipts`;
- the old table is removed only by a separately classified contract migration;
- unexpected legacy rows fail closed without data loss;
- mixed-version deployment, traffic and backup gates are documented;
- fresh-install and upgraded schemas agree that `inbox_events` is absent.

## Implementation evidence
- Migration `000064_retire_legacy_inbox_events.sql` locks the exact legacy table, validates an impossible constraint to prove it empty, then drops it atomically.
- Catalog metadata marks the change high-risk and contract-only, disables rolling compatibility flags, requires a backup, and names the empty-table, zero-traffic and minimum-binary gates.
- PostgreSQL tenancy checks now assert that the legacy relation is absent while retaining the forced-RLS and immutability checks for `inbox_receipts`.
