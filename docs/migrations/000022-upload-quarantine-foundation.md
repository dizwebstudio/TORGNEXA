# Migration 000022 — Upload Quarantine Foundation

Task `088a` adds the tenant-scoped canonical `uploads` state table. It is an expand-only, high-risk migration because upload authorization is a security boundary and production rollout requires a backup checkpoint.

The schema reserves the full parent Task-088 lifecycle, but the database trigger deliberately permits only `RECEIVED -> QUARANTINED`. No application repository method can release an object in this stage. Task `088b` must replace the foundation trigger only in the same change that adds validation/scanner evidence and the authorized release path.

Forced PostgreSQL RLS is enabled for `organization_id/workspace_id`. Quarantine and released object keys are exact server-derived paths containing both tenant identifiers and `UploadID`; filenames cannot become storage paths. Delete/truncate are rejected while the security workflow owns the record.

Rollback disables the new upload admission path and leaves the additive table intact. Do not drop upload state while quarantined objects exist; backup/restore must reconcile object storage with canonical upload metadata before exposure.
