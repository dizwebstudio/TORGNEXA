# Migration 000060: connector account capabilities

This expand migration adds the append-only `connector_account_capability_history` table for Task 107. Each revision is tenant-scoped, bound to a connector account version and stores a complete manifest capability snapshot with explicit `read`/`write`, risk and approval metadata.

Existing accounts are intentionally not backfilled. Until an administrator saves a selection, the application interprets the missing snapshot as default deny. The table uses forced RLS, forbids update/delete/truncate for public roles and retains prior revisions for audit and rollback evidence.

Rollback is operational: save an empty selection to deny all connector operations. The expand table is retained so historical authorization evidence is not destroyed.
