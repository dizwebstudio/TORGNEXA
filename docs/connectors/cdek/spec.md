# CDEK Connector Spec

Family: `logistics`. Reference carrier connector proving rates, shipment lifecycle, tracking, cancellation, labels, pickup points and return flow.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. OAuth client credentials and all remote tariff/PVZ identifiers remain provider-local. Host-side account service mapping converts remote tariff ids into canonical TORGNEXA service codes before routing.

Official documentation: https://apidoc.cdek.ru/
