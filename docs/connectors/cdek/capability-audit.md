# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. CDEK OAuth client credentials are checked against the token endpoint and a bounded city-directory read; the access token is discarded after the probe. Rates, shipment, tracking, labels, pickup points and returns remain SDK capabilities but are not yet runtime routes. All remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing. No browser-cookie automation, private editor endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://apidoc.cdek.ru/
