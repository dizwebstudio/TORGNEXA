# ADR-0141 — Чтение партий из архива Почты России

Status: Accepted

## Context

The Russian Post API exposes the archive as a separate read endpoint. The
existing batch directory reads active batches through `/1.0/batch`, so using
that route for archived data would hide provider lifecycle state and could
not distinguish an archived response from an active one.

## Decision

Admit `logistics.batches.archive.read` only for the qualified Russian Post
runtime. Register `GET /api/v1/logistics/batches/archive` as an authenticated,
tenant-scoped operation requiring the account capability and a bounded
`limit` query parameter.

The host adapter calls the official `GET /1.0/archive` endpoint over fixed
HTTPS egress with the existing callback-scoped credentials. It accepts at
most 100 rows, requires unique valid batch names and statuses, and validates a
non-negative shipment count. The normalized response contains only the batch
reference, status, shipment count and observation time.

## Alternatives considered

Reusing the active `/1.0/batch` reader would conflate two provider lifecycle
directories. Passing arbitrary archive filters or URLs from the browser would
weaken the egress and tenant boundaries. Returning raw archive objects would
expose unnecessary provider fields and possible order data; both alternatives
are rejected.

## Consequences

Operators can inspect archived batches from the integration surface and use
the separate approved restore action when needed. The view is intentionally
read-only and bounded; archive operations not covered by an explicit
capability remain fail-closed.

## Security and privacy impact

The capability is read-only and account-scoped. Fixed HTTPS egress, response
size bounds, duplicate detection and projection minimization prevent the
archive endpoint from becoming an unbounded data export.

## Compatibility impact

The additive capability, route and generated SDK method do not change active
batch reads or write operations. Existing accounts are unchanged until the
new capability is enabled.

## Migration and data impact

No migration is required. Archive rows are returned as a bounded live
projection and are not persisted as canonical batch state.

## Operational impact

Provider failures are reported as bounded logistics errors and can be retried
by the read UI. Live qualification requires a non-production Russian Post
account and an evidence bundle based on the official [API «Отправка» specification](https://otpravka.pochta.ru/specification).
