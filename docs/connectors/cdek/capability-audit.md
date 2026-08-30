# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. CDEK OAuth client credentials are checked against the token endpoint and a bounded city-directory read; the access token is discarded after the probe. The host transport and application runtime now expose a bounded read-only delivery-point route when `pickup.points.read` is explicitly enabled. Rates, shipment, tracking, labels and returns remain closed until current fixtures, canonical service mapping, idempotent host routing and non-production qualification are retained. All remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing. No browser-cookie automation, private editor endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://apidoc.cdek.ru/
