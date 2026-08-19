# Migration 000012 — Connector SDK account model

Task 010 extends the existing tenant-scoped `connector_accounts` table without
renaming/dropping legacy columns.

Expand additions:

- optimistic `version` and `updated_at`;
- normalized `health_status`, `health_reason_code`, `health_checked_at`;
- canonical family check including `fx` and `notification`;
- connector manifest-id validation on the legacy `provider` column;
- account lifecycle/version/health monotonicity trigger;
- no-delete/no-truncate protection and indexes for manifest/status/health.

The table still stores only `secret_reference`; no password, access token,
refresh token, API key, client secret, certificate private material or master
key column is introduced.

Existing rows remain readable/writable by old binaries because all new columns
have expand-compatible defaults. New Task-010 writers additionally honor the
version/lifecycle guards. A future contract migration may rename `provider` to
`connector_id` only after old readers/writers are retired.
