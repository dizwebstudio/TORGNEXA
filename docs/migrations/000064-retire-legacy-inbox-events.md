# Migration 000064 — retire legacy inbox_events

Migration `000064_retire_legacy_inbox_events.sql` is a high-risk contract migration that removes the pre-Task-009, tenantless `inbox_events` compatibility placeholder. Production consumers have used the tenant-scoped immutable `inbox_receipts` table since migration `000008`; repository runtime code contains no reads or writes of the old table.

## Contract gates

- a tested backup checkpoint exists for the target environment;
- every supported binary uses the `inbox_receipts` contract;
- database traffic monitoring confirms no access to `inbox_events` during the qualification window;
- `inbox_events` is empty.

The migration takes a lock bounded by `lock_timeout`, adds and validates `CHECK (false) NOT VALID`, and only then drops the table. The validation can succeed only for an empty table. Any unexpected legacy row aborts and rolls back the whole migration, preserving the table and its contents for explicit investigation.

## Compatibility and recovery

This contract step is intentionally incompatible with old readers and writers and must not run in a mixed-version fleet. No API, event, or inbox receipt schema changes. If deployment fails before commit, PostgreSQL rolls the transaction back. After commit, normal recovery is forward-only; restore the verified checkpoint only when environment recovery policy requires it. `inbox_receipts` and its immutable deduplication evidence are unaffected.
