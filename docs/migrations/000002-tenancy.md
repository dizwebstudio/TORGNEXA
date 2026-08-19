# Migration 000002 — tenant hierarchy and isolation

## Current / target state

`000001_platform.sql` created organizations and workspaces but omitted stores,
composite tenant foreign keys, optimistic versions, and database-level tenant
isolation. `000002_tenancy.sql` completes that foundation and applies forced RLS
to the existing tenant-owned platform tables.

## Compatibility and preflight

The migration is additive and transactional. Before applying it, verify that
existing organization/workspace IDs are canonical UUIDv7 or ULID values, names
are trimmed/non-empty, lifecycle metadata is valid, and connector/outbox/audit
workspace pairs identify a real workspace in the same organization. The
migration validates every staged check/FK and rolls back as a unit if legacy
data violates an invariant; it never silently repairs or deletes such rows.

The unique workspace key and validation scans take bounded table locks. Apply
with the included five-second lock timeout during a controlled maintenance
window if these early foundation tables already contain material data.

## Expand phase

- add `status`, `version`, and `updated_at` to organizations/workspaces;
- create stores and its scoped uniqueness/indexes;
- add composite workspace foreign keys;
- validate all staged constraints;
- enable and force RLS, then create exact tenant policies;
- force deny-all RLS on the tenantless placeholder inbox table; Task 009 later keeps it deny-all and adds `inbox_receipts` via an additive expand migration.

There is no backfill with invented business meaning. Column defaults only
represent the safe initial lifecycle/version for existing valid rows.

## Deployment order

Deploy the typed tenancy repository and migration as one foundation change
before any API begins reading these tables. Every application transaction sets
both tenant GUC values locally. The application role must be non-owner and must
not have `BYPASSRLS`.

## Observability and verification

After applying in a real PostgreSQL test environment, verify:

- migration transaction success and constraint `convalidated=true`;
- `relrowsecurity=true` and `relforcerowsecurity=true` for all tenant tables and
  the reserved inbox table;
- expected policies in `pg_policies`;
- at the `000002` schema boundary no policy exists for `inbox_events`, so application reads/writes are denied; contract migration `000064` later removes the qualified empty placeholder;
- same-tenant reads succeed;
- missing settings, mixed organization/workspace pairs, and cross-tenant store
  IDs return no rows;
- transaction pooling does not retain scope after commit/rollback.

No production data may be used for these tests; create two synthetic tenants.

## Rollback / repair

Before commit, any failure rolls the complete transaction back. After commit,
do not drop stores/columns or cascade-delete data. Repair invalid legacy rows in
a separately reviewed migration, restore the application version if needed,
and keep RLS enabled. Emergency RLS changes require a security incident record,
explicit operational role, and immediate revalidation; they are not a normal
rollback mechanism.

## Backup checkpoint

Take and verify the environment-appropriate PostgreSQL backup/checkpoint before
applying to a database with persistent tenant data. Task 027 owns the full
backup/restore rehearsal; this migration must not be promoted without that gate
once real data exists.
