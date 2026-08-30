# Task 145: CDEK and Деловые Линии delivery verification

## Status

`repository-complete` — 2026-08-28. CDEK is moved from `planned` to the
separate Delivery surface and the Деловые Линии connector is added. Both
providers expose encrypted account credentials and an authenticated health
check. CDEK and Деловые Линии additionally expose the bounded read-only
`pickup.points.read` route; CDEK also exposes the bounded read-only
`logistics.rates.read` preview and the bounded read-only
`logistics.track.read` status lookup. Shipment operations remain
qualification-gated.

## Objective

Make the existing CDEK reference visible and honest in the frontend, and add
Деловые Линии with a bounded terminal/PUDO read route without claiming
unqualified shipment, label or product-sync routes. The tracking lookup is
read-only and accepts an existing CDEK remote reference.

## Acceptance

- CDEK and Деловые Линии appear under `Интеграции → Доставка`;
- CDEK checks OAuth client credentials and a bounded city-directory request;
- Деловые Линии checks appkey/PAT through the official v4 login endpoint;
- CDEK and Деловые Линии pickup-point reads use bounded country/city filters and
  normalized response fields; CDEK uses a short-lived OAuth token and Деловые
  Линии uses its official directory reference;
- CDEK rate previews accept at most 50 parcels, normalize at most 100 tariff
  results and return fixed-decimal money with neutral option identifiers;
- CDEK tracking accepts one shipment reference, normalizes at most 100 status
  records and returns the latest neutral status without the raw provider body;
- credentials stay callback-scoped and session/access tokens are discarded;
- runtime support is `separate_surface/logistics` with only CDEK's
  `logistics.rates.read`/`logistics.track.read`/`pickup.points.read` and Деловые Линии's
  `pickup.points.read`, with no operational writes;
- deterministic SDK/conformance evidence and documentation are synchronized.

## Qualification boundary

Live shipment creation, labels, returns and the final carrier qualification of
write operations need current provider fixtures, tenant-scoped
non-production credentials and an idempotent host bridge. Until then, the UI
clearly offers account setup, «Проверить», the bounded read-only CDEK/
Деловые Линии directory route, the CDEK rate preview and the CDEK tracking
lookup only when each capability is explicitly enabled.
