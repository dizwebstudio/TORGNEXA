# ADR-0096: Pre-v1 migration baseline and verified rebaseline

Status: Accepted

## Context

Before the first production v1 release TORGNEXA accumulated 74 PostgreSQL migration files while building the platform incrementally. That chain is useful development history, but it is unnecessarily noisy for fresh Community/CI installations, slower to review/bootstrap, and forces a new installation to create and later amend/retire structures that are already known in their final pre-v1 state.

Simply deleting or concatenating files is unsafe. Atomic migrations `000004` onward write their own immutable metadata to `migration_history`; naive concatenation would create duplicate history versions. Existing development databases may already contain the exact 74-row history and must not become unusable merely because the fresh-install path is compacted.

## Decision

Before production v1, make `migrations/` a compact **11-file active baseline**. Keep bootstrap migrations `000001`–`000003` byte-for-byte. Deterministically squash legacy 004–074 into eight source-order-preserving components, retaining legacy contract migration 064 as a distinct contract-phase component. Generated groups remove only the legacy outer transaction boundaries and per-file `migration_history` inserts, then add one outer transaction and one active baseline history insert.

Move the original 74 SQL files and original catalog to `migrations_legacy_pre_v1/` as immutable evidence. `migrations/baseline-manifest.json` binds each active component to all archived source files/checksums and pins the archived catalog digest. `make migration-baseline` regenerates/checks the baseline deterministically and rejects archive or generated drift.

A development database already at exact legacy head 74 is not silently accepted. The migrator detects the legacy row count and blocks with a rebaseline instruction unless an explicit acknowledgement is supplied. `rebaseline-pre-v1.sh` verifies all 74 version/name/checksum records and final-schema sentinels, archives those records to `migration_history_legacy_pre_v1`, then transactionally stamps the 11 active baseline records and `migration_baseline_evidence`. Partial, unknown or drifted histories remain blocked.

This history rewrite is a one-time pre-v1 exception only. After the first production v1 release, active migration history and applied SQL are immutable; future changes are new forward migrations after the active baseline head.

## Migration and data impact

The compact baseline preserves legacy statement order and therefore final schema semantics. It does not rewrite application/domain rows. Fresh databases execute 11 active SQL files. A verified existing development database changes migration metadata only: all 74 prior rows are preserved in the legacy archive table before the active 11-row baseline is stamped.

## Compatibility impact

Fresh install compatibility improves because only the compact active catalog is mounted. Existing exact-head development databases remain supportable through the explicit verified rebaseline. Unknown or partially applied legacy histories intentionally fail closed and require restore/manual review rather than inferred repair.

## Security and privacy impact

The rebaseline requires database-owner migration credentials and an explicit acknowledgement token. It validates immutable checksums before mutation, performs the archive/stamp in one transaction, writes no credentials/PII to evidence, and revokes public access to baseline/legacy-history metadata. No RLS/business data boundary is bypassed by application code.

## Operational impact

`make migrations` now validates both the active catalog and deterministic baseline equivalence. `make migration-rebaseline` is a one-time pre-v1 operation for already-created development databases; it must not be included in normal production startup. Community fresh installs use only the 11 active files. The original archive is excluded from the application Docker build context.

Backup/restore and upgrade rehearsals must distinguish active baseline version `000011` from historical legacy source head `000074`. The archived source head remains useful forensic evidence but is no longer the runtime migration count.

## Testing and rollback

Repository checks verify exactly 11 active SQL files, 74 immutable archived SQL files, every legacy SHA-256, baseline manifest bindings, active/legacy deployment TSV parity, and migration policy validation. Domain migration tests continue reading the archived source files, while baseline equivalence proves those source statements are represented in the active generated groups.

Before the first production release, rollback of the repository change is possible by restoring the prior source tree. A database that has already been rebaselined retains all 74 original rows in `migration_history_legacy_pre_v1`, so investigation/reconstruction evidence is preserved. After v1, the baseline itself is immutable.

## Consequences

Fresh TORGNEXA installations traverse 11 migration files instead of 74 while keeping the entire pre-v1 development chain reviewable. The trade-off is a one-time lineage transition for existing development databases and an explicit distinction between active baseline version numbers and archived legacy source version numbers.

## Alternatives considered

One giant `baseline.sql` was rejected because it is harder to review and reason about. Keeping all 74 files active was rejected because it carries development evolution into every fresh install. Deleting the legacy chain was rejected because it destroys checksum and incident evidence. Silently treating a 74-row database as equivalent to an 11-row database was rejected because it would weaken migration-history integrity.
