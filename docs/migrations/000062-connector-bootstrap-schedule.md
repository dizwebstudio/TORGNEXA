# Migration 000062 — connector bootstrap and schedules

Task 108 adds three tenant-scoped control-plane tables: immutable metadata-only dry-run evidence, optimistic per-account schedules and lease-based resumable dispatch jobs. All tables use forced RLS. They store counts, versions, checkpoints and safe error codes only; connector credentials, remote payloads and raw provider errors are prohibited.

The cross-tenant scheduler can discover work only through `claim_connector_sync_jobs`. This bounded `SECURITY DEFINER` function enqueues due schedules, repairs expired leases and returns tenant identity with each lease. The worker must reapply that scope before reading policies, capabilities or creating reconciliation runs. Production deployment must grant this function only to the scheduler runtime role.

This is an expand migration. Old API/worker binaries ignore the new tables, and a new API binary on the old schema remains healthy because Task-108 repositories are invoked only by the additive endpoints. Deploy migration 000062 before starting the new scheduler loop; the scheduler intentionally fails closed when its claim function is unavailable. Rollback is operational: stop the scheduler and disable schedules through the API. Retain preview/job evidence until the audit retention policy permits removal. A backup checkpoint is required before rollout because the migration introduces a privileged dispatch boundary.

Checksum: `2d2774bb884fcb3fe1807e63210c078ac43a940c03ce989a408f3fea7313c987`.
