# Data Retention & Archival

Retention is configured per data family and constrained by legal/business obligations.

Hot operational data stays in PostgreSQL; analytical retention in ClickHouse is policy-driven; binary evidence/media uses S3 lifecycle/versioning; Kafka retention is not a compliance archive.

Archival jobs are resumable, audited and verify object/checksum counts. Deletion propagates to search/cache/derived analytics unless an explicit legal hold applies. Legal holds are scoped, time-bounded when possible and visible in audit.


## Task 060 policy metadata

`privacy_retention_policies` is the canonical tenant-scoped policy registry. Each row is bound to an active processing purpose and canonical data class, records `retention_days`, the future disposition, and whether a legal hold may override ordinary expiry. Registry updates use monotonic versions and rows are retired rather than hard-deleted.

Task 061 is repository-complete: expiry/hold/subject-request/tenant-deletion workflows use resumable per-store checkpoints and append-only evidence across authoritative and derived stores. Production deployments must bind the coordinator to every store in their topology. Kafka retention remains transport configuration, not a compliance archive.
