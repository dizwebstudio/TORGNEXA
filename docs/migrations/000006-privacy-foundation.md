# Migration 000006 — privacy foundation

`000006_privacy_foundation.sql` is an atomic expand migration for Task 060.

It adds tenant-scoped `privacy_purposes` and `privacy_retention_policies`, forced RLS, policy-shape constraints, purpose/class retention enforcement, optimistic version guards, irreversible retirement, and hard-delete/truncate rejection.

The migration stores governance metadata only. It does not create subject records or raw PII columns and does not implement deletion/subject-request execution; those are Task 061 responsibilities.

The migration is classified high risk because privacy/retention metadata is a security/compliance control. The migration catalog therefore requires a backup checkpoint before deployment.
