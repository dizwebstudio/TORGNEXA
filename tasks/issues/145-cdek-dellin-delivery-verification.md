# Task 145: CDEK and Деловые Линии delivery verification

## Status

`repository-complete` — 2026-08-28. CDEK is moved from `planned` to the
separate Delivery surface and the Деловые Линии connector is added. Both
providers expose encrypted account credentials and an authenticated health
check; shipment operations remain qualification-gated.

## Objective

Make the existing CDEK reference visible and honest in the frontend, and add
Деловые Линии without claiming unqualified shipment, label or product-sync
routes.

## Acceptance

- CDEK and Деловые Линии appear under `Интеграции → Доставка`;
- CDEK checks OAuth client credentials and a bounded city-directory request;
- Деловые Линии checks appkey/PAT through the official v4 login endpoint;
- credentials stay callback-scoped and session/access tokens are discarded;
- runtime support is `separate_surface/logistics` with no operational writes;
- deterministic SDK/conformance evidence and documentation are synchronized.

## Qualification boundary

Live shipment creation, rates, tracking, labels, returns and pickup reads need
current provider fixtures, tenant-scoped non-production credentials and an
idempotent host bridge. Until then, the UI clearly offers only account setup
and «Проверить».
