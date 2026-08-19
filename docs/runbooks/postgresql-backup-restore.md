# PostgreSQL Backup, Restore, and PITR Runbook

PostgreSQL is the transactional system of record. A backup is accepted only
after its exact bytes have been integrity-checked and restored into an isolated
target. A successful upload, snapshot, or `pg_dump` exit by itself is not
restore evidence.

This runbook covers PostgreSQL 18 as currently pinned by the runtime inventory.
Restore to the same major version and an equal or newer patch release first;
perform major-version upgrades only through the separate upgrade procedure.

## Recovery objectives

These are conservative backup objectives until Task 066 approves product SLOs.
A deployment may choose stricter values, but must record them and configure
monitoring against the chosen values.

| Tier | Maximum RPO | Maximum restore RTO | Minimum mechanism |
|---|---:|---:|---|
| Local/development | 24 hours | 4 hours | Daily logical custom-format dump |
| Shared staging | 1 hour | 2 hours | Daily physical base backup plus continuous WAL archive |
| Production baseline | 15 minutes | 2 hours | Continuous WAL archive, daily verified base backup, immutable encrypted copy in a separate failure domain |

RPO measures the latest committed transaction that may be lost. RTO runs from
the restore declaration until database integrity, tenant isolation, and the
application readiness checks all pass. High availability is separate from
backup recovery and does not relax either objective.

## Non-negotiable controls

- Use a dedicated backup/replication role and a root-owned libpq service/passfile
  with mode `0600`; never put a password in a command, URI, shell history, log,
  evidence manifest, or CI variable expansion.
- Production backup bytes are confidential. Encrypt before leaving encrypted
  staging, use a KMS/HSM-controlled key reference, and store an immutable copy
  in a separate account/failure domain. Backing up a KMS key reference does not
  back up private signing keys.
- Enable continuous WAL archival and alert when archival age approaches the
  selected RPO. `archive_command = 'true'`, a local-only archive, or an uploader
  without durable acknowledgement is a failed configuration.
- Never overwrite an existing backup or restore in place. Create a new staging
  directory and a new isolated database/cluster. Destruction of an old cluster
  is a later, explicitly approved operation after cutover.
- Verify SHA-256, the PostgreSQL `backup_manifest`, WAL continuity, PostgreSQL
  major version, and available capacity before starting recovery.
- Production data must not be copied to development or CI. A non-production
  restore requires approved anonymization/minimization and the same access,
  retention, audit, and deletion controls as its source.
- Disable application/network access during restore. The restored application
  role must remain `NOBYPASSRLS`; validation is performed before traffic is
  switched.

## Executable synthetic drill

Run the repository drill locally or through CI:

```bash
make backup-restore-runtime
```

The script resolves the digest-pinned PostgreSQL image from the release
inventory, starts a no-network/no-port read-only container, and uses only an
ephemeral `tmpfs`. It performs all of the following:

1. applies every current migration and creates two synthetic tenants;
2. creates a custom-format logical dump and restores it into a new database;
3. takes a physical `pg_basebackup` with streamed WAL and SHA-256 manifest;
4. verifies the base backup with `pg_verifybackup`;
5. records an inclusive recovery target LSN, writes a post-target transaction,
   archives the required WAL, and restores to the target;
6. proves the target transaction exists and the post-target transaction does
   not;
7. proves tenant RLS still returns only the selected tenant;
8. corrupts a backup copy and requires `pg_verifybackup` to reject it.

To retain sanitized evidence, provide a new absolute file below an explicitly
bounded output root:

```bash
restore_evidence_dir="$(mktemp -d /tmp/torgnexa-restore-evidence.XXXXXX)"
TORGNEXA_SAFE_OUTPUT_ROOT="$restore_evidence_dir" \
  ./scripts/check-postgres-backup-restore.sh \
  --evidence-file "$restore_evidence_dir/postgresql-restore-evidence.json"
```

The output must match
`contracts/operations/postgresql-restore-evidence.schema.json`. It contains
only image/tool versions, LSN/timeline values, artifact sizes and hashes, and
check results. It never contains credentials, storage URLs, tenant data, or raw
database output. Synthetic evidence is marked `ephemeral_test`; it is not proof
that production storage, encryption, retention, or credentials work.

## Development logical backup

Use PostgreSQL client tools from the same major version. Configure connection
details in a protected libpq service file and create a new private directory:

```bash
export PGSERVICEFILE=/run/secrets/torgnexa-pg-service.conf
export PGSERVICE=torgnexa_backup
logical_backup_dir="$(mktemp -d /var/tmp/torgnexa-logical.XXXXXX)"
chmod 0700 "$logical_backup_dir"
pg_dump --dbname="service=$PGSERVICE" \
  --format=custom --compress=zstd:9 --no-owner \
  --file="$logical_backup_dir/database.dump"
pg_restore --list "$logical_backup_dir/database.dump" >/dev/null
sha256sum -- "$logical_backup_dir/database.dump" \
  >"$logical_backup_dir/SHA256SUMS"
```

The logical dump is for local/development portability, not production PITR. It
does not include cluster-global roles or continuous WAL. Preserve grants only
when the referenced roles are managed in the target; otherwise bootstrap roles
from reviewed infrastructure configuration before restoring.

Restore into a new empty database and never use `--clean` against an in-use
database:

```bash
export PGSERVICE=torgnexa_restore_admin
sha256sum --check "$logical_backup_dir/SHA256SUMS"
pg_restore --list "$logical_backup_dir/database.dump" >/dev/null
createdb --maintenance-db="service=$PGSERVICE dbname=postgres" \
  torgnexa_restore_candidate
pg_restore --dbname="service=$PGSERVICE dbname=torgnexa_restore_candidate" \
  --exit-on-error --single-transaction --no-owner \
  "$logical_backup_dir/database.dump"
```

If any command fails, quarantine the artifact and keep the previous validated
backup. Do not retry a checksum mismatch as though it were a transient error.

## Production physical backup

Managed PostgreSQL must have provider PITR enabled, cross-account/region backup
retention where supported, and a provider-specific restore drill. Record the
snapshot/base-backup identifier, timeline/LSN, WAL coverage, encryption key
reference, immutable retention, and provider job IDs in restricted operational
evidence. A control-plane “available” state is not enough; restore it.

For self-managed PostgreSQL, continuous archive publication is deployment
specific and must be reviewed separately. Once the durable archive is healthy,
the PostgreSQL-native base-backup portion is:

```bash
export PGSERVICEFILE=/run/secrets/torgnexa-pg-service.conf
export PGSERVICE=torgnexa_replication
physical_backup_dir=/srv/torgnexa-backup-staging/base-NEW-BACKUP-ID
install -d -m 0700 -- "$physical_backup_dir"
pg_basebackup --dbname="service=$PGSERVICE" \
  --pgdata="$physical_backup_dir" --format=plain \
  --checkpoint=fast --wal-method=stream \
  --manifest-checksums=SHA256 --no-password
pg_verifybackup "$physical_backup_dir"
sha256sum -- "$physical_backup_dir/backup_manifest" \
  >"$physical_backup_dir/backup_manifest.sha256"
```

Publish/encrypt the immutable artifact only after `pg_verifybackup` succeeds.
The publication job must return the durable object version/digest; record that
identifier outside the backup object itself. Periodically test that the KMS key
and storage account remain recoverable under disaster credentials.

## Point-in-time restore

1. Declare the incident and choose a target timestamp or LSN from authoritative
   audit/incident evidence. Preserve the original cluster read-only when safe.
2. Provision an isolated target with no application ingress/egress. Confirm its
   disk capacity and exact PostgreSQL major version.
3. Retrieve/decrypt one immutable base backup and all WAL required from its
   start LSN through the target. Verify the recorded object digests and run
   `pg_verifybackup` before copying bytes into a new empty `PGDATA`.
4. Configure a restore command that fails when an expected WAL object is absent,
   then set exactly one reviewed recovery target. For an LSN target:

```text
restore_command = '<reviewed downloader> %f %p'
recovery_target_lsn = '<reviewed target LSN>'
recovery_target_inclusive = on
recovery_target_action = promote
```

5. Create `recovery.signal`, start PostgreSQL, and monitor the server log. A
   missing WAL segment, unexpected timeline, checksum mismatch, or target that
   precedes the base-backup consistency point aborts the drill.
6. Do not route traffic merely because PostgreSQL accepts connections. Complete
   the validation below and obtain the incident/release approver's decision.

## Mandatory post-restore validation

- `pg_is_in_recovery()` is false only after the reviewed target was reached and
  promotion was intentional.
- The chosen target marker/audit record is present and a known post-target
  record is absent.
- All migrations expected by the application release exist; constraints are
  validated and no interrupted contract/backfill step is silently skipped.
- Tenant tables retain both `relrowsecurity` and `relforcerowsecurity`; the
  application role is `NOBYPASSRLS`. Test same-tenant success, cross-tenant
  denial, mixed organization/workspace denial, and transaction-local scope
  reset using synthetic validation rows.
- Domain row counts and append-only audit/outbox invariants are reconciled.
  ClickHouse, caches, and search remain derived and are rebuilt/reconciled after
  PostgreSQL is accepted.
- Application health, write/read smoke, outbox publication, error rate, and
  lag are acceptable before cutover. Keep the old cluster and restore evidence
  for the incident retention window.

## Scheduling and evidence

- Run the synthetic drill on every main CI and release-candidate workflow.
- Run a real staging restore at least monthly and after PostgreSQL image,
  storage, KMS, archiver, or topology changes.
- Run a production-grade isolated restore at least quarterly; do not attach
  application traffic or expose restored PII beyond the approved drill team.
- Alert on base-backup age, WAL archive age/failure, missing immutable copy,
  failed checksum/manifest verification, failed restore drill, and measured RPO
  or RTO outside the deployment objective.
- Retain sanitized evidence longer than the backup it qualifies. Store the
  evidence digest, base/WAL object versions, timeline/target, versions,
  start/end times, measured RPO/RTO, approver, and result. Keep credentials and
  raw database content out of evidence.

Failure of any required check blocks release and backup rotation. Preserve the
last known-restorable chain until a newer chain has independently passed.
