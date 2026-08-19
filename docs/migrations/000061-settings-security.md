# Migration 000061 — settings security

This expand migration creates `settings_identity_sessions` and append-only `settings_login_events` for Task 103. Both tables have forced tenant RLS. Session revocation is a monotonic state transition; history rows cannot be updated, deleted or truncated by the application role.

Only SHA-256 references to provider session and subject identifiers are stored. Bearer tokens, raw OIDC identifiers, IP addresses and raw User-Agent strings are excluded. Rollback is forward-only: stop writers/readers first and retain the tables as security evidence until the configured retention window expires.
