# CDEK Connector Spec

Family: `logistics`. The current production surface is credential verification
plus a bounded read-only pickup-point directory; the SDK candidate also proves
rates, shipment lifecycle, tracking, cancellation, labels and return flow
without making those operations available to application callers.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. Paste credentials as JSON `{ "client_id": "…", "client_secret": "…" }`. The host exchanges them at `/v2/oauth/token`, performs a bounded `/v2/location/cities?size=1` read for health and can perform a bounded `/v2/deliverypoints` read for a requested country/city, then discards the access token. OAuth client credentials and all remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing. The delivery-point adapter is available through the protected application route only when its capability is explicitly enabled; live provider qualification is still required before enabling it in a production account.

The pickup-point operation is available through the protected
`GET /api/v1/logistics/pickup-points` route only when the account explicitly
enables `pickup.points.read`. Shipment writes, rates, labels, tracking,
returns and webhooks remain qualification-gated.

Official documentation: https://apidoc.cdek.ru/
