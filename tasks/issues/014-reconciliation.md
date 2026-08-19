# Task 014

On-demand reconciliation framework, drift records and safe policy actions; reusable by scheduled scans.

## Acceptance
Implementation + tests + updated contracts/docs; run required checks.

## Repository completion — 2026-08-10

Repository implementation is complete.

- One provider-neutral runner supports `incremental`, `scheduled_full`, and `on_demand` modes.
- Durable run progress/cursor supports restart/resume without duplicating drift evidence.
- Drift records cover content drift, missing/orphan/duplicate mappings, status mismatch, and stale remote observation.
- Drift evidence stores bounded identifiers/fingerprints/revisions/status/cardinality only; raw payload bodies, credentials, and raw remote errors are excluded.
- Safe auto-fix is fail-closed: only unambiguous mapping creation or Task-013 source-of-truth/direction-authorized content/status repair can execute automatically.
- Notify, approval, and explicit ignore are separate policy actions; action attempts are append-only and external effects use deterministic idempotency keys.
- Migration `000025_reconciliation.sql` adds forced-RLS runs/drifts/action history with immutable evidence and one-way drift resolution.
- Draft 2020-12 contracts cover reconciliation runs, drift evidence, and action receipts.

Task 014 does not implement WB/Ozon/ERP transport logic. Reference connectors consume this framework next.
