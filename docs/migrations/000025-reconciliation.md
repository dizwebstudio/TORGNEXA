# Migration 000025 — Reconciliation

Task 014 adds the durable reconciliation control plane on top of Task 013 sync primitives.

- `reconciliation_runs` stores resumable incremental, scheduled-full and on-demand scan progress.
- `reconciliation_drifts` stores bounded drift evidence only: identifiers, fingerprints, revisions/status and mapping cardinality; no raw remote payload/error or credentials.
- `reconciliation_actions` is append-only remediation attempt evidence using deterministic external idempotency keys.
- All tables use forced RLS and explicit organization/workspace predicates in the PostgreSQL repository.
- Completed runs are immutable. Drift evidence is immutable; only a one-way open -> resolved disposition is allowed.
- The migration is expand-only. Rollback is operationally performed by disabling reconciliation scheduling while retaining evidence.

Checksum: `d7a5786086d01c4d957684cc71dde0a8c04b728e36425b5585ae78fa6e1428d0`.
