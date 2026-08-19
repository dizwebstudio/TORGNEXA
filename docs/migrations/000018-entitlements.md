# Migration 000018 — entitlements

Expand-only migration creating `entitlement_rules`, `entitlement_quota_policies`, `entitlement_quota_counters`, and `entitlement_quota_usage`.

Rules and policies are append-only versions. Usage evidence is append-only and idempotent. Counters are tenant-scoped mutable enforcement state guarded against backwards movement. All four tables use forced RLS; hard deletion/truncation is denied in normal application paths.

Rollback of the binary leaves the additive schema intact. Before deployment qualification, verify concurrent quota consumers against real PostgreSQL and verify logical/PITR restore preserves both counters and usage evidence.
