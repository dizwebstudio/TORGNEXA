# CDEK Connector Spec

Family: `logistics`. The current production surface is credential verification
plus bounded read-only pickup-point, rate-preview and tracking reads, as well as
approval-bound cancellation and shipment-creation routes. Shipment creation is
available through the host-side asynchronous route only after capability,
approval, idempotency and encrypted-payload checks. Labels are available through
the bounded `GET /api/v1/logistics/labels` read route: the adapter creates a CDEK
barcode print request and returns a neutral PDF artifact reference. Returns and
webhooks remain qualification-gated.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. Paste credentials as JSON `{ "client_id": "…", "client_secret": "…" }`. The host exchanges them at `/v2/oauth/token`, performs a bounded `/v2/location/cities?size=1` read for health and can perform a bounded `/v2/deliverypoints` read for a requested country/city, then discards the access token. OAuth client credentials and all remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing. The delivery-point adapter is available through the protected application route only when its capability is explicitly enabled; live provider qualification is still required before enabling it in a production account.

The pickup-point operation is available through the protected
`GET /api/v1/logistics/pickup-points` route only when the account explicitly
enables `pickup.points.read`. A bounded rate preview is available through
`POST /api/v1/logistics/rates` when `logistics.rates.read` is enabled. It
accepts up to 50 parcels and returns up to 100 neutral options. Money is
parsed as fixed decimal provider text into minor units; provider tariff ids
are not returned by the application route. A bounded tracking read is
available through `GET /api/v1/logistics/tracking` when
`logistics.track.read` is enabled. It selects the latest status from at most
100 provider status records and returns no raw provider payload. Shipment
creation is routed through `POST /api/v1/logistics/shipments`; the worker uses
the official CDEK order contract and records an ambiguous remote outcome as
`unknown` without a blind retry. Label reads resolve a shipment number to its
UUID when necessary, submit the official barcode-print request and return only
the resulting PDF artifact reference. Returns and webhooks remain
qualification-gated.

Official documentation: https://apidoc.cdek.ru/
