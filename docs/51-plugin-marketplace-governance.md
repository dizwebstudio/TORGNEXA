# Plugin Marketplace Governance

Task `078` turns the Task-025 plugin security descriptor into a distributable, reviewable and revocable marketplace contract without creating another plugin runtime.

## Trust is visible, not implied

Every listing shows the immutable artifact SHA-256, publisher/key identity, semantic version, SDK major, license, security contact and trust level:

- `official` — TORGNEXA-owned/released publisher identity;
- `verified` — third-party publisher with reviewed identity/provenance;
- `community` — reviewed marketplace package without verified builder identity;
- `private` — tenant-private reviewed package, visible only inside its organization/workspace.

Trust does **not** grant runtime authority. A valid Ed25519 signature proves who signed exact bytes; it does not approve capabilities for a tenant.

## Review gate

All marketplace listings require successful Task-064 conformance, supply-chain/malware checks, license review and a reviewed security contact. `official` and `verified` additionally require subject-bound SBOM and provenance verification consistent with Task 065. Review evidence is immutable metadata for the exact artifact digest.

## Consent and updates

Installation UI must show the complete signed request before activation:

- functional Connector SDK capabilities;
- required secret classes;
- exact TLS egress host/port destinations;
- memory/CPU/wall/output/concurrency ceilings.

Tenant consent is a Task-025 `PermissionGrant` bound to one plugin id, version and SHA-256 digest. A different digest/version always requires a fresh consent; no update inherits an old grant automatically.

`AssessUpdate` separately highlights privilege growth: newly requested capabilities, secret classes, network destinations, increased isolation ceilings, publisher identity changes and trust downgrade. This makes capability escalation an explicit reapproval event rather than a hidden side effect of update.

## Revocation

Revocations are append-only evidence and are checked before runtime admission:

1. artifact revocation — blocks one plugin id + digest globally;
2. publisher-key revocation — blocks every artifact signed by the compromised publisher key;
3. installation revocation — blocks one tenant consent without affecting other tenants.

Revocation never hard-deletes listing, consent or historical review evidence.

## Admission chain

`reviewed listing -> explicit tenant consent -> revocation check -> Task-025 digest/Ed25519/grant verification -> inert AdmissionPlan -> Task-029 sandbox`

The marketplace cannot execute code, read secrets, open sockets, access filesystem/environment or bypass Task-029. Task-064 remains the connector negative-conformance authority. Task-065 remains the release/supply-chain evidence authority.

## Persistence

Migration `000028_plugin_marketplace_governance.sql` separates global public listings/revocations from tenant-private versions/consents/installation revocations. Private/tenant tables use explicit organization/workspace keys and `FORCE ROW LEVEL SECURITY`; all governance evidence is append-only.

Public `GET /api/v1/plugins` is typed by `contracts/plugins/marketplace-page-v1.schema.json` and exposes trust/requested authority, not credentials or signing secrets.
