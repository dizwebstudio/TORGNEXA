# Legacy pre-v1 migration chain

This directory is an immutable archive of the original TORGNEXA migration chain `000001`–`000074`.
It is **not** mounted as the active runtime migration directory and must never be executed on a fresh database.

Fresh pre-v1 installations use the compact 11-file baseline in `../migrations/`.
The archived chain exists for:

- checksum/equivalence evidence for the baseline squash;
- one-time verification of development databases already at legacy head `000074`;
- historical migration tests and incident/audit review.

Do not edit these files. `scripts/generate-pre-v1-baseline.py --check` verifies all 74 archived SQL checksums against this directory's `catalog.json` and proves that the active baseline is a deterministic statement-order-preserving squash of the legacy source ranges.

A legacy development database must be verified and stamped with `deploy/postgres/rebaseline-pre-v1.sh` before it can use the compact active catalog. The procedure archives all 74 existing `migration_history` rows in `migration_history_legacy_pre_v1` before replacing the active history with the 11 baseline stamps. This exception is allowed only before the first production v1 release.
