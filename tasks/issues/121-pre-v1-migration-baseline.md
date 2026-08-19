# Task 121: Pre-v1 Migration Baseline / Squash

## Status
`done` — repository implementation complete on 2026-08-18.

## Objective
Replace the 74-file pre-production development migration chain with a compact, reviewable fresh-install baseline without losing the immutable legacy evidence or breaking already-created development databases.

## Deliverables

### 1. Compact active baseline
- [x] keep bootstrap migrations `000001`–`000003` byte-for-byte;
- [x] squash legacy `000004`–`000074` into eight statement-order-preserving components;
- [x] active runtime inventory is exactly **11 SQL files**;
- [x] each generated atomic component records exactly one new baseline migration-history row;
- [x] retain the legacy contract step separately so destructive retirement is not mislabeled as expand.

### 2. Immutable legacy evidence
- [x] move the original 74 SQL files and original catalog to `migrations_legacy_pre_v1/`;
- [x] verify every archived SQL SHA-256 against the archived catalog;
- [x] bind every baseline component to its legacy source version/file/checksum set in `migrations/baseline-manifest.json`;
- [x] exclude the legacy archive from application Docker build context and runtime migration mount.

### 3. Existing development database continuity
- [x] add fail-closed detection of a legacy 74-row migration history;
- [x] add explicit one-time `rebaseline-pre-v1.sh` guarded by an acknowledgement token;
- [x] verify all 74 version/name/checksum rows before any history rewrite;
- [x] verify final-schema sentinel relations before stamping;
- [x] archive all original rows in `migration_history_legacy_pre_v1` in the same transaction;
- [x] stamp the 11 active baseline rows and durable `migration_baseline_evidence` only after verification;
- [x] leave unknown/partial histories blocked.

### 4. Gates and documentation
- [x] add deterministic `make migration-baseline` / `scripts/check-pre-v1-baseline.sh`;
- [x] include baseline verification in `make migrations` and full `make check`;
- [x] update Community deployment catalog checks for active and legacy catalogs;
- [x] preserve legacy domain-migration tests against the archived immutable chain;
- [x] update upgrade/release/handoff documentation for active baseline vs legacy head semantics.

## Active baseline

| Active | Component | Legacy source |
|---|---|---|
| 000001 | platform | 000001 |
| 000002 | tenancy | 000002 |
| 000003 | migration_framework | 000003 |
| 000004 | security_eventing | 000004–000008 |
| 000005 | commerce_core | 000009–000017 |
| 000006 | operations_foundation | 000018–000027 |
| 000007 | commerce_extensions | 000028–000040 |
| 000008 | regulated_integrations | 000041–000054 |
| 000009 | control_plane | 000055–000063 |
| 000010 | legacy_contract | 000064 |
| 000011 | runtime_operations | 000065–000074 |

## Safety invariants
- the squash does not reorder legacy DDL statements inside any source range;
- legacy per-file history inserts are removed from generated groups so one active file creates one active history row;
- original 74 SQL files are never edited after archival;
- a legacy database is never rebaselined merely because it has 74 rows: every name/checksum and final-schema sentinel must match;
- original history is archived before the active history is stamped;
- the rebaseline is explicitly pre-v1-only; after production v1, applied migration history is immutable and schema changes use new forward migrations;
- no application/domain persisted data is rewritten by the squash itself.

## Acceptance
- active `migrations/*.sql` count is 11;
- archived legacy SQL count is 74 and all checksums match the archived catalog;
- deterministic baseline regeneration/check passes;
- migration catalog checker passes with 11 active entries;
- Community deployment catalog checks pass for both active and legacy metadata;
- existing legacy migration tests point at immutable archive evidence;
- exact architecture review covers the Task 120 → Task 121 diff.
