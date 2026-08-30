# Migration 000028 — Payment reconciliation worker

Phase: `expand`. Risk: `medium`. Dependency: `000027`.

Adds the security-definer discovery function used by the bounded payment
reconciliation worker and a tenant-local lookup index for provider refund IDs.
The function returns only organization/workspace scopes that have an active
payment connector account; the worker re-enters each returned scope before
reading connector or payment data. Account identifiers, credentials and
payment data never cross the discovery boundary.

The migration is additive and does not change payment rows or status semantics.
The worker remains compatible with an older schema: when the function is not
available, the reconciliation component records no scope work and continues
serving the rest of the worker. The sweep itself is limited to 48 hours and at
most once every five minutes; it applies changes through the existing
optimistic, audited `ChangePaymentStatus` path.

Rollback is application rollback only. The function can remain in the schema
because it has no persisted state and is safe for older binaries to ignore.
