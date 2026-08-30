# ADR 0118: Inventory Forecast and Guarded Auto-Replenishment

## Status

Accepted for the Task 165 foundation; runtime and production qualification remain gated.

## Context

The existing replenishment package provides a small advisory velocity formula.
Task 165 needs a provider-neutral forecast and stock projection that can use
exact quantities, returns-aware demand, freshness evidence and workspace
policies without creating a second inventory or purchase-order lifecycle.

## Decision

1. Forecast runs, points, projections and recommendations are derived,
   tenant-scoped facts. PostgreSQL stores their input digest, algorithm/policy
   versions and bounded quality evidence; WMS inventory remains authoritative.
2. The baseline algorithm is deterministic and explainable: the latest
   normalized net demand is carried forward and the observed maximum is the
   upper (P90) bound. It uses fixed-point `domain.Decimal`/`domain.Quantity`;
   no floating point or unversioned model code is allowed.
3. Every workspace policy selects one explicit mode:
   `recommendation_only` (default), idempotent `draft_po`, or narrowly
   qualified `auto_submit`. Recommendation generation never performs a remote
   write. Draft/submit paths must use the existing procurement state machine,
   policy/approval, capability and reconciliation boundaries.
4. Unknown, stale or insufficient inputs fail closed. Shortfall is retained
   explicitly even when projected stock is clamped to zero. MOQ/case-pack and
   unit checks are deterministic and provider-neutral.
5. New persistence is additive (`000032_replenishment_forecast_planning.sql`),
   forced-RLS and append-only for recommendation history. Outbox/Inbox,
   durable scheduling, API/UI and live connector qualification are follow-up
   slices and are not implied by this foundation.

## Alternatives considered

- Treat forecast as inventory truth: rejected because it would corrupt WMS
  ledger semantics and make uncertain external data authoritative.
- Put policy in each connector: rejected because provider branches bypass
  common approval, budget and idempotency controls.
- Enable automatic PO submission from a feature flag: rejected; auto-submit
  requires current capability/conformance evidence, approval, spend caps and a
  manual kill switch.

## Compatibility impact

The migration is expand-only and preserves existing readers and writers. The
existing Task 053 advisory API remains compatible; no public route or connector
SDK interface changes in this foundation slice.

## Migration and data impact

Migration `000032_replenishment_forecast_planning.sql` adds tenant-scoped
forecast, projection, policy, recommendation-history and draft-plan tables. It
does not rewrite inventory, WMS or procurement history.

## Security and privacy impact

All new records carry organization/workspace scope and forced RLS. Stored data
is limited to exact quantities, bounded reason text and digests; credentials,
tokens and raw provider/model payloads are excluded.

## Operational impact

The current delivery slice is advisory and safe to deploy behind the existing
route/policy controls. Production admission still requires durable worker,
procurement, connector, Compose, load/chaos and recovery evidence described by
Task 165.

## Consequences

Forecast uncertainty and stockout shortfall remain visible and reproducible,
while no forecast can become inventory truth or submit a purchase order. Later
runtime slices must preserve the explicit modes and fail-closed boundaries.
