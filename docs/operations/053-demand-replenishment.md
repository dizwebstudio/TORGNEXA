# Demand & Replenishment Planning

Task `053` defines the original advisory contract. Task `165` extends it with
an exact, provider-neutral forecast/projection foundation in
`internal/platform/replenishment` and migration
`000032_replenishment_forecast_planning.sql`.

## Safety invariants

Replenishment is advisory by default. Every forecast run and recommendation
pins an immutable input digest, algorithm version and bounded quality evidence.
The deterministic baseline carries the latest normalized net demand forward and
uses the observed maximum as its upper bound. Returns are netted explicitly;
unknown or stale input is not replaced with zero. Stock projections clamp only
the displayed available balance and retain a separate shortfall quantity.

Operating modes are explicit per policy: `recommendation_only` (default),
idempotent `draft_po`, and narrowly qualified `auto_submit`. The current
foundation only builds derived facts and proposed recommendations. It does not
run a scheduler, create/submit a remote PO, or authorize inventory changes.

## Persistence

PostgreSQL expand migration: `000032_replenishment_forecast_planning.sql`.
Runs, points, projections, policies, recommendation history and draft-plan
metadata are tenant-scoped with forced RLS; recommendation history is
append-only. In-memory implementations in tests are reference semantics, not
production durability.

The repository runtime slice now exposes `GET/POST /api/v1/replenishment` and
the frontend route `/replenishment`. The operator enters normalized demand,
returns, available stock, confirmed inbound and supplier offer; the API stores
an immutable digest, exact p50/p90 forecast, explicit shortfall and a proposed
recommendation. The recommendation is always `recommendation_only` in this
slice and cannot submit a purchase order.

The remaining external release gates are provider-backed input adapters,
approval-bound procurement submission, scheduled catch-up/load/chaos evidence
and live connector qualification. See
`tasks/issues/165-stock-forecast-auto-replenishment.md` and ADR 0118.
