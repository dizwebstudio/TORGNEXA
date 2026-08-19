# Multi-tenancy, Entitlements & Billing

Hierarchy: Organization -> Workspace -> Store/BusinessUnit -> ConnectorAccount.

## Tenant boundary

- `Organization` is the top-level tenant and billing boundary.
- `Workspace` is the mandatory authorization/operational boundary inside one
  organization.
- `Store`/`BusinessUnit` belongs to exactly one `(organization, workspace)`;
  its normalized `code` is unique within that scope.
- IDs are canonical UUIDv7 or ULID values. Names and codes are internal
  operational metadata, not legal-party identifiers and not a place for
  secrets or customer PII.

Every application lookup takes a typed tenant scope containing both IDs. The
scope comes from reviewed authentication/role mapping, never from an arbitrary
request body. A nonexistent record and a record owned by another tenant both
return the same `tenancy.ErrNotFound` result.

## PostgreSQL enforcement

Migration `000002_tenancy.sql` adds the store schema, optimistic versions,
lifecycle states, composite tenant foreign keys, and forced RLS on the current
tenant-owned platform tables. Task 009 introduced the real tenant-scoped
`inbox_receipts` contract instead of destructively repurposing the pre-Task-009
`inbox_events` placeholder during an expand migration. Contract migration
`000064` removes that empty placeholder after fleet and traffic qualification.
Before each repository query, one read-only
transaction executes transaction-local settings:

```sql
SELECT set_config('app.organization_id', $1, true),
       set_config('app.workspace_id', $2, true);
```

The final `true` is mandatory: tenant state must disappear at transaction end
and must never leak through a pooled connection. Queries still include explicit
organization/workspace predicates. The application database role must not own
tables and must not have `BYPASSRLS`; schema migration, backup, and reviewed
repair use separate operational identities.

Global workers must not disable RLS ad hoc. They either iterate explicit tenant
scopes or use a separately reviewed, least-privileged operational port with
audit evidence. Provisioning a new organization/workspace is likewise an
explicit privileged workflow, not a general repository bypass.

Status values are `active`, `suspended`, and `archived`. Hard deletion and
cross-store retention are deferred to the coordinated privacy/retention
workflow; no task may silently cascade-delete tenant history.

Entitlements/quotas are checked by a provider-neutral feature service, not scattered `if plan ==` branches.
Task 028 implements this as versioned feature rules plus atomic UTC-window quota policies/usage. Missing rules/policies fail closed; Task 086 Cloud Billing may synchronize these records later but is not a runtime dependency.

Meter candidates: connector accounts, stores, SKU count, event/API volume, media storage, report retention, AI/automation operations.

Community must remain functional self-hosted; Cloud/Enterprise can add managed operations, HA, advanced auth, isolation, support and higher quotas.
