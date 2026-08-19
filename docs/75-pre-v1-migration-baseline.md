# Pre-v1 migration baseline

TORGNEXA squashes the original `000001`–`000074` development chain before the first production v1 release. The active runtime catalog now contains **11 migrations**:

1. `000001_platform.sql`
2. `000002_tenancy.sql`
3. `000003_migration_framework.sql`
4. `000004_security_eventing.sql` — legacy 004–008
5. `000005_commerce_core.sql` — legacy 009–017
6. `000006_operations_foundation.sql` — legacy 018–027
7. `000007_commerce_extensions.sql` — legacy 028–040
8. `000008_regulated_integrations.sql` — legacy 041–054
9. `000009_control_plane.sql` — legacy 055–063
10. `000010_legacy_contract.sql` — legacy 064
11. `000011_runtime_operations.sql` — legacy 065–074

The first three files are retained byte-for-byte because they create/adopt `migration_history`. The remaining files are generated deterministically from the archived source statements with legacy per-file `migration_history` inserts removed and one new atomic baseline history insert added per active file.

## Safety model

`migrations_legacy_pre_v1/` stores the immutable original 74-file chain and catalog. It is outside the runtime migration mount and excluded from the application Docker build context.

`migrations/baseline-manifest.json` pins:

- active baseline count;
- legacy head/version count;
- legacy catalog SHA-256;
- every legacy source version/file/checksum used by each baseline component;
- each generated baseline SHA-256.

Run:

```bash
make migration-baseline
```

The check regenerates the compact baseline deterministically and fails on any legacy checksum drift, source-range drift, active SQL drift, wrong inventory count, or manifest mismatch.

## Existing development databases

A database already carrying legacy `migration_history` versions 1–74 must **not** silently continue against the new 11-entry catalog.

The one-time pre-v1 procedure is:

```bash
TORGNEXA_ALLOW_PRE_V1_REBASELINE=I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY \
  docker compose run --rm --entrypoint /deploy/postgres/rebaseline-pre-v1.sh migrate
```

or:

```bash
make migration-rebaseline
```

The rebaseline refuses to run unless all 74 rows exactly match the archived version/name/checksum catalog and final-schema sentinel relations exist. In one transaction it:

1. locks `migration_history`;
2. copies all 74 rows to `migration_history_legacy_pre_v1`;
3. creates/updates `migration_baseline_evidence`;
4. replaces active history with the exact 11 baseline catalog stamps;
5. verifies both active and archived row counts.

This is an explicit **pre-v1-only** history rewrite. After the first production v1 release, applied migration history is immutable and future schema changes are new migrations beginning after the active baseline head.

## Fresh installation and equivalence

Fresh databases execute only the 11 active files. The grouped files preserve the original DDL statement order, so the final schema state follows the same source sequence as legacy `000001`–`000074`, while bootstrap/test/runtime no longer traverses 74 migration files.

The legacy archive remains evidence, not an alternate install path.
