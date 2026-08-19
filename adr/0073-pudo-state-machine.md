# ADR 0073: Pickup and drop-off operations state machine

## Status
Accepted — Task 73.

## Context
Task 075 needs own and external pickup-point operations, capacity, arrival/readiness/issue/expiry/return transitions and hooks into payment/fiscal/logistics while preserving deterministic operational state.

## Decision
1. Model pickup points with ownership kind, capacity and active state.
2. Model pickup orders with explicit state transitions: created, arrived, ready, issued, expired, return-pending and returned.
3. Reject invalid transitions and capacity overflow.
4. Execute issue/return side effects only through injected hooks so fiscal/payment/logistics implementations remain separate.
5. Persist an append-only order event history.

## Alternatives considered
- Free-form mutable status strings: rejected because invalid transitions would be possible.
- Inline carrier/payment/fiscal vendor logic: rejected by architecture boundaries.
- Silent over-capacity acceptance: rejected.

## Compatibility impact
The PUDO module is additive and consumes existing provider-neutral primitives; existing order/WMS contracts remain intact.

## Migration and data impact
Migration `000048` adds pickup registry, pickup orders and append-only pickup-order events with forced tenant RLS.

## Operational impact
Operators need capacity monitoring, expiry sweeps, reconciliation of external pickup availability and alerts for failed issue/return hooks.

## Security and privacy impact
Pickup data is tenant-scoped and purpose-limited. Side-effect credentials remain in their dedicated integration boundaries; event history supports audit without raw secrets.

## Consequences
TORGNEXA gains deterministic pickup operations for own/external points and a stable seam for reference logistics/PUDO integrations.
