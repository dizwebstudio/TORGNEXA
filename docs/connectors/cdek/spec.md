# CDEK Connector Spec

Family: `logistics`. The current production surface is credential verification;
the SDK candidate also proves rates, shipment lifecycle, tracking, cancellation,
labels, pickup points and return flow without making those operations available
to application callers.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. Paste credentials as JSON `{ "client_id": "…", "client_secret": "…" }`. The host exchanges them at `/v2/oauth/token`, performs a bounded `/v2/location/cities?size=1` read, and discards the access token. OAuth client credentials and all remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing.

Official documentation: https://apidoc.cdek.ru/
