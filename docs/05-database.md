# Database Design

PostgreSQL owns operational state. JSONB is for variable remote metadata, not core schema.

The API process opens PostgreSQL through the bounded `database/sql` pool in
`internal/platform/postgres/database`, backed by pgx stdlib. Startup proves
connectivity with a bounded `PingContext` and fails closed without exposing the
secret-bearing DSN. Pool size, idle capacity, connection lifetime and idle
time are explicit configuration; the pool closes with the API lifecycle.
Schema compatibility remains the responsibility of the canonical migration
job that must complete before API startup. Tenant-scoped repositories continue
to set RLS context transaction-locally so pooled connections cannot retain one
request's organization/workspace context for the next request.

Required patterns: tenant boundary, timestamps, optimistic versions, source/lineage metadata, connector remote-ID mapping.

The tenancy root is `organizations -> workspaces -> stores`. Tenant-owned rows
carry both `organization_id` and `workspace_id`; composite foreign keys prevent
a child from combining an organization with another organization's workspace.
Application lookups must include both values even when PostgreSQL row-level
security is enabled. RLS is defense in depth and does not replace authorization
derived from the authenticated identity.

Current platform tables include organizations, workspaces, stores, connector_accounts, products, offers, prices, warehouses, inventory_positions, orders, order_items, publications, outbox_events, inbox_receipts, approval_requests and audit_records. The bootstrap-only `inbox_events` relation was retired by contract migration `000064`.

`audit_records` is an append-only tenant-scoped evidence table for application access. New records require actor, source, action, resource, correlation, risk and a bounded redacted JSON summary. Application roles receive SELECT/INSERT behavior only; mutation/truncation is blocked by RLS/trigger guards. See `docs/migrations/000004-audit-base.md` and `contracts/audit/audit-record.schema.json`.

## Integration state center (migration 000035)

Task 168 stores only rebuildable integration-state metadata: immutable snapshots,
status transitions, action receipts and a coalescing recompute queue. These rows
are tenant-scoped with forced RLS and never replace connector account,
credential, health, capability, sync or reconciliation sources. See
`docs/migrations/000035-integration-state-center.md`.

Tenancy identifiers are canonical UUIDv7 or ULID text so they remain portable
and time-sortable without relying on database sequences. Lifecycle changes use
`status`, `updated_at`, and an optimistic `version`; archival is not a hidden
hard delete.

ClickHouse stores sales facts, stock/price history, fees, ads, social and integration metrics. Transactional writes never block on ClickHouse.

Migrations are forward-first, deterministic and designed for rolling deployment where feasible.

Backup and restore follow the executable PostgreSQL runbook in
`docs/runbooks/postgresql-backup-restore.md`. Production uses verified physical
base backups plus continuous WAL archival; a logical dump alone is not PITR.
Release candidates must pass the isolated restore/PITR drill and high-risk
migrations require a separately verified environment backup checkpoint.

Every SQL file is registered exactly once in `migrations/catalog.json` with an
immutable SHA-256 and expand/migrate/contract metadata. `make migrations`
rejects drift and unsafe phase rules; `migration_history` rejects unknown/gapped
database prefixes, while `backfill_jobs` provides bounded checkpoints and
fenced leases. See `docs/47-upgrade-migrations.md`.

`secret_references` and `secret_versions` implement Task 021 secret isolation. Normal business rows, including `connector_accounts`, persist only the opaque tenant-bound `secret_reference`; encrypted versions carry AES-GCM ciphertext/nonces plus a non-secret external `key_id`. Master keys and plaintext credentials are not PostgreSQL data. See `docs/migrations/000005-secrets-provider.md` and `contracts/secrets/secret-reference.schema.json`.


`privacy_purposes` and `privacy_retention_policies` implement Task 060 governance metadata. Both are organization/workspace scoped with forced RLS; purpose/legal-basis/notice/consent metadata is separated from subject PII, and retention rows are constrained to classes allowed by the active purpose. See `docs/migrations/000006-privacy-foundation.md` and `contracts/privacy/`.

`outbox_events` is upgraded by Task 008 into the transactional DB→EventBus hand-off. New rows carry the canonical immutable EventBus envelope plus tenant-scoped ready/lease/publication metadata; relay claims use short `FOR UPDATE SKIP LOCKED` leases and application runtime cannot hard-delete event intent. Legacy bootstrap rows remain distinguishable by `event_envelope IS NULL` during the expand phase. See `docs/migrations/000007-transactional-outbox.md`.

`inbox_receipts` implements Task 009 consumer idempotency. It stores no event payload, only tenant/logical-consumer identity, event ID/type, canonical-envelope SHA-256 fingerprint and bounded processing metadata. Matching duplicates skip business code; collisions fail closed. Receipts are immutable with forced SELECT/INSERT RLS. Contract migration `000064` removes the empty legacy `inbox_events` placeholder after minimum-binary, zero-traffic and backup qualification. See `docs/migrations/000008-inbox-idempotency.md`, `docs/migrations/000064-retire-legacy-inbox-events.md` and `contracts/transport/inbox-receipt.schema.json`.


`products`, `offers`, and `connector_entity_mappings` implement Task 004. Product/Offer use optimistic versions, forward-only archival lifecycle, forced tenant RLS, immutable local identity, and no application hard-delete path. GTIN is validated with the GS1 modulo-10 check digit. Remote IDs exist only in `connector_entity_mappings`, never in canonical Product/Offer columns. Catalog mutations and their versioned EventBus intent commit atomically through `outbox_events`. See `docs/migrations/000009-catalog-domain.md` and `contracts/catalog/`.

`prices`, `warehouses`, and `inventory_positions` implement Task 005. Price money is persisted as `minor_units bigint + currency`; inventory decimal quantities are persisted as exact signed coefficient + scale + unit components matching the Task-076a representation, never as binary floating point. Forced tenant RLS, composite Offer/Warehouse foreign keys, immutable identities, optimistic versions, non-negative/reserved<=on-hand checks, and no application hard-delete path are enforced in PostgreSQL. Price/inventory mutations atomically append Task-003 audit evidence and Task-008 outbox event intent. See `docs/migrations/000010-price-inventory.md`, `contracts/pricing/`, and `contracts/inventory/`.

## Orders (migration 000011)

`orders` and `order_items` are tenant-scoped with forced RLS and composite workspace/Offer foreign keys. OrderItem rows and the Order commercial snapshot are immutable; only normalized Order status/version may advance. A deferred constraint trigger validates aggregate money totals from immutable lines at commit. Exact quantity uses coefficient+scale and money uses bigint minor units plus Order currency; no floating-point columns are permitted.

Task 010 hardens `connector_accounts` as the Connector SDK account model. The legacy physical `provider` column is the canonical manifest id for expand compatibility; account status/version and normalized health metadata are persisted under forced tenant RLS. Only opaque `secret_reference` may represent credentials, and hard delete/truncate is blocked. See `docs/migrations/000012-connector-sdk.md` and `contracts/connectors/`.

## Worker runtime leases (migration 000067)

Task 113 adds `worker_runtime_jobs` for shared worker-fleet coordination. The
queue stores only tenant/item identities, availability, fenced lease metadata,
attempt count and a bounded machine error code. It enables and forces RLS.

Cross-tenant discovery is permitted only through narrow `SECURITY DEFINER`
functions that list active outbox/webhook scopes or claim reconciliation/upload
identities. These functions return identity/lease metadata only. The worker then
uses the normal tenant-scoped repositories, which set organization/workspace RLS
context transaction-locally before reading or mutating domain state. See
`docs/migrations/000067-worker-runtime-dispatch.md`.
