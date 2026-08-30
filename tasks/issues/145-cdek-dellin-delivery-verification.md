# Task 145: CDEK and Деловые Линии delivery verification

## Status

`repository-complete` — 2026-08-30. CDEK is moved from `planned` to the
separate Delivery surface and the Деловые Линии connector is added. Both
providers expose encrypted account credentials and an authenticated health
check. CDEK and Деловые Линии additionally expose the bounded read-only
`pickup.points.read` route; Деловые Линии also exposes bounded
`logistics.rates.read` and `logistics.track.read` reads, while CDEK also exposes
the bounded read-only `logistics.rates.read` preview and the bounded read-only
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
- Деловые Линии tracking accepts one document reference, normalizes at most 100
  status records from `statuses_history.json` and returns the latest neutral
  status without the raw provider body;
- Деловые Линии rate previews accept at most 50 parcels, normalize one official
  calculator result and return fixed-decimal RUB money with a neutral option
  identifier;
- credentials stay callback-scoped and session/access tokens are discarded;
- runtime support is `separate_surface/logistics` with CDEK's bounded read
  capabilities and the approval-bound `logistics.shipment.cancel` route
  admitted; Деловые Линии remains pickup/rate/tracking read-only and CDEK shipment
  creation, labels, returns and webhooks remain closed;
- deterministic SDK/conformance evidence and documentation are synchronized.

## Qualification boundary

Live shipment creation, labels, returns and the final carrier qualification of
write operations need current provider fixtures and tenant-scoped
non-production credentials. The cancellation route already has the durable
host bridge, approval gate, idempotency receipt and unknown-outcome handling;
the UI exposes it only when the account capability and a matching approval are
present. Account setup, «Проверить», the bounded CDEK/Деловые Линии directory
route, the CDEK/Деловые Линии rate previews, and CDEK/Деловые Линии tracking
lookups remain available as before.
