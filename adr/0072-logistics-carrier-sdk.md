# ADR 0072: Provider-neutral logistics carrier SDK

## Status
Accepted — Task 72.

## Context
Task 074 needs carrier rating, shipment creation, labels, tracking, cancellation and returns without embedding a particular carrier into fulfillment logic.

## Decision
1. Add capability-specific logistics request/response interfaces to Connector SDK v1.
2. Normalize parcel, address, exact cost, SLA and tracking observations.
3. Keep shipment state tenant-scoped and remote tracking evidence append-only.
4. Rank comparable rates only when currency/cost semantics are compatible; avoid implicit FX.

## Alternatives considered
- Carrier-specific fields in Core: rejected.
- Compare unlike currencies with implicit FX: rejected.
- Treat local shipment state as authoritative over carrier tracking: rejected.

## Compatibility impact
The interfaces are additive and optional; no existing connector/runtime or order contract is changed.

## Migration and data impact
Migration `000047` adds tenant-scoped shipments and append-only tracking evidence. No destructive backfill is required.

## Operational impact
Reference carrier adapters must separately qualify auth, rate limits, label media handling, webhook/poll reconciliation and cancellation/return semantics.

## Security and privacy impact
Addresses are purpose-limited fulfillment data under tenant scope. Credentials stay host-owned; labels/artifacts should follow secure artifact handling.

## Consequences
Fulfillment and PUDO can consume one normalized carrier boundary while concrete carriers remain replaceable providers.
