# Migration 000034 — Channel unit economics

Migration 34 adds tenant-scoped channel dimensions and order attribution,
immutable historical COGS snapshots, calculation-run metadata and bounded
quality issues. Every table has forced RLS and composite organization/workspace
keys. Cost evidence and published runs are append-only; corrections use a new
snapshot/run and never rewrite Orders, Payments or SettlementEntry.

The migration also expands the settlement kind check additively for
`logistics`, `storage`, `advertising`, `penalty`, `compensation` and
`withholding`. Existing rows remain valid. Apply the migration before enabling
the factual report and take the usual PostgreSQL backup checkpoint on the small
VPS Compose deployment.
