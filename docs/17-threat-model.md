# Threat Model Starter

Assets: connector tokens, signing keys, customer/order data, pricing/inventory authority, payment/fiscal/compliance data, audit.

Boundaries: clients->API, API->workers/Kafka, connector->remote, n8n/OpenClaw->TORGNEXA, TORGNEXA->Signing Service, tenant A vs B.

Threats: credential leakage, cross-tenant confused deputy, malicious plugin, AI unauthorized write, replay/duplicate remote write, webhook spoofing, compromised service account, supply-chain compromise, forged remote status, audit tampering.

Mitigations: scoped credentials, tenant authz everywhere, webhook verification, idempotency, approval policies, redaction, dependency/image scanning, append-oriented audit, signing isolation.

Tenant lookup specifically uses a validated `(organization_id, workspace_id)`
scope derived from authentication. Repository SQL repeats both predicates and
PostgreSQL applies forced row-level security from transaction-local settings.
Missing and cross-tenant records deliberately return the same opaque result to
avoid an identifier-enumeration oracle. Application roles must not have
`BYPASSRLS`; migration/repair roles are separate and audited.
