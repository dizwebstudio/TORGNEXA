# ADR 0089: Provider-neutral CRM family and Bitrix24 universal API

## Status
Accepted for Task 097.

## Context
TORGNEXA had commerce, ERP, social, payment and logistics connector families but no CRM family. Treating Bitrix24 as ERP or marketplace would distort capability semantics and make future amoCRM/Odoo CRM integrations provider-specific. Bitrix24 also now recommends universal `crm.item.*` methods while several entity-specific list APIs are deprecated.

## Decision
Add an additive Connector SDK v1 family `crm` with four provider-neutral capabilities: `crm.entities.read`, `crm.entities.write`, `crm.productrows.read`, and `crm.productrows.write`. Root SDK interfaces stay frozen.

Admit provider `bitrix24` through OAuth2 bearer calls to the exact configured portal host. Entity operations use `crm.item.list/get/add/update` with system entity type IDs (lead 1, deal 2, contact 3, company 4). Product rows use `crm.item.productrow.list/set` and owner short codes. Create reconciliation uses Bitrix24 origin fields with `originatorId=TORGNEXA` and a stable `originId` supplied by the host.

Bitrix24 list methods have a fixed 50-record remote page. TORGNEXA cursors therefore retain both remote `start` and an intra-page offset, so a smaller caller page size never drops records.

## Security and privacy impact
OAuth token bytes stay behind `SecretAccessor` and are copied only into the transport's dedicated bearer field. v1 does not request contact/company `fm` multifields because retrieving them requires wildcard selection that may return broader CRM data than the connector needs. Error descriptions from the provider are not propagated into normalized errors.

## Compatibility impact
This is additive SDK v1 surface. Existing provider families, manifests and root interfaces are unchanged. Future CRM providers can reuse the same family and contracts.

## Alternatives considered
- Reuse the `erp` family. Rejected because CRM entities and product-row semantics are not ERP synchronization contracts.
- Use legacy entity-specific `crm.deal.*`, `crm.contact.*`, and `crm.company.*` APIs. Rejected because the universal item API is the forward-compatible integration surface.
- Put the OAuth token in webhook-style URLs. Rejected because bearer credentials must stay behind `SecretAccessor` and out of URLs, logs, and persisted configuration.

## Migration and data impact
No database migration is required. Existing mapping, sync, inbox, audit, and reconciliation storage remains authoritative. The change adds SDK enum/capability values and a provider implementation only; existing provider records and manifests do not change.

## Operational impact
Operators configure the exact Bitrix24 portal host plus an OAuth secret reference. Provider calls are rate-limited by the existing connector runtime and normalize remote authentication, throttling, validation, and ambiguous-write failures. The connector can be disabled per account without a schema rollback.

## Consequences
TORGNEXA gains a reusable CRM orchestration layer and a Bitrix24 implementation on current REST semantics. OAuth installation/refresh, event subscriptions and richer PII/contact-point synchronization remain separate qualified work.
