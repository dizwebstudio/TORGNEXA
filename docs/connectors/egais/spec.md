# EGAIS UTM Connector Spec

Family: `government`. Regulated EGAIS document/status, inventory/reference and reconciliation adapter through the officially documented Universal Transport Module boundary.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. UTM is the local official transport boundary. Regulated writes require an existing signed/released artifact, approval reference and idempotency key; unsupported document kinds remain explicit and remote tickets/status are authoritative.

Official documentation: https://egais.ru/files/Doc_utm_420.pdf
