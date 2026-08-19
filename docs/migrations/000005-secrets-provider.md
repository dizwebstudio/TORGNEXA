# Migration 000005 — secrets provider

`000005_secrets_provider.sql` is an atomic expand migration for Task 021.

It adds stable tenant-scoped `secret_references` plus immutable encrypted `secret_versions`. No plaintext token/password/client-secret columns are introduced. The only durable credential payload is AES-256-GCM ciphertext; PostgreSQL stores an external `key_id` but never a master key.

The migration uses forced organization/workspace RLS. Reference rows allow select/insert/update only for lifecycle changes, with a trigger freezing identity/class and enforcing monotonic version/revocation transitions. Version rows allow select/insert only and reject update/delete/truncate even for privileged table access through the normal schema path.

Catalog risk is `high`, so the migration framework requires a backup checkpoint before application in release/deployment rehearsal even though the schema change is expand-only.
