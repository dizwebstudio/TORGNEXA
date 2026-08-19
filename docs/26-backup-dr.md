# Backup & Disaster Recovery

Protect PostgreSQL, Kafka, ClickHouse, S3/media, identity configuration, and
critical secret metadata. PostgreSQL is the transactional recovery anchor; a
cache, analytics store, or Kafka topic is never substituted for committed
PostgreSQL invariants.

## Current executable baseline

The [PostgreSQL backup/restore runbook](runbooks/postgresql-backup-restore.md)
defines provisional tier objectives, logical development restore, production
physical backup/PITR, immutable/encrypted storage controls, post-restore tenant
validation, drill cadence, and sanitized evidence. The digest-pinned synthetic
drill runs in main CI and every release candidate. A backup is not accepted
until its exact chain has been restored and verified.

Task 066 may tighten RPO/RTO when product SLOs are approved. It may not weaken
the default restore controls or retroactively treat untested backups as valid.

## Recovery ownership by system

| System | Authority and recovery rule |
|---|---|
| PostgreSQL | Operational system of record. Continuous WAL plus verified physical base backup is the production baseline; restore/PITR is required before release qualification. |
| Kafka | Durable event transport and replay window, not transactional truth. Preserve topic configuration, ACLs, schemas, retention, and offsets as operational metadata. Reconcile/replay from authoritative outbox/domain state after PostgreSQL recovery; never infer missing business commits from Kafka alone. |
| ClickHouse | Analytics/history only. Rebuild replayable projections from Kafka/PostgreSQL and back up non-replayable historical partitions with integrity/restore tests. It never blocks transactional PostgreSQL recovery. |
| S3/media | Enable versioning, lifecycle, encryption, and immutable retention/object lock where supported. Restore by explicit object version; quarantine/security-release state must be reconciled before content is exposed. |
| Identity configuration | Export reviewed realm/client/role mappings and back up the authoritative identity database/configuration. Restored federation/JIT mappings remain default deny until reviewed. Raw credentials are not configuration evidence. |
| Secret/signing metadata | Back up secret references, versions, policy, and KMS/HSM metadata separately. Private signing keys are never exported through generic backup jobs; key-provider disaster recovery is a separately controlled ceremony. |
| Valkey/search/cache | Disposable derived state. Rebuild after authoritative services recover; never restore it as the sole source of critical state. |

Deployment-specific Kafka, ClickHouse, object-storage, identity-provider, and
KMS commands depend on components that are not yet selected or implemented.
They must be added and exercised with synthetic fixtures when those components
enter scope; placeholder commands or a successful configuration export do not
qualify as a restore drill.

## Cross-system recovery order

1. Recover credentials/KMS access required to read encrypted backup metadata,
   without exporting private signing material.
2. Restore and accept PostgreSQL at the reviewed point in time.
3. Restore Kafka configuration and replay/reconcile durable events from the
   accepted PostgreSQL/outbox boundary.
4. Restore versioned S3/media objects and reapply quarantine/release policy.
5. Rebuild ClickHouse, search, and caches; reconcile derived watermarks.
6. Restore/review identity mappings before enabling external login.
7. Validate end-to-end outbox, connector, webhook, audit, and tenant isolation
   before reopening traffic.

Every production deployment must name an owner and approver, select objectives
no weaker than the baseline, monitor backup/WAL age, keep an immutable copy in a
separate failure domain, and retain sanitized restore evidence. Failure of a
required restore, unavailable decryption key, missing WAL/object version, or an
unreconciled tenant boundary blocks cutover and release.
