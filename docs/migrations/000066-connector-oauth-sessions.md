# Migration 000066: connector OAuth sessions

Task 106 adds a tenant-scoped one-time OAuth session store and the `oauth_state`
secret class. PostgreSQL stores the SHA-256 digest of `state`, callback binding,
actor, account version and expiry; raw state and the PKCE verifier remain only in
the encrypted `SecretProvider` record.

The migration is additive and keeps old readers and writers compatible. Session
rows are protected by forced RLS, cannot be deleted or truncated through normal
roles, and allow exactly one `pending` → `consumed` transition before expiry.

Rollback is application-first: deploy the previous binary, retain the table and
encrypted evidence until its ten-minute validity window and audit retention have
elapsed, then remove it only in a separately reviewed contract migration.
