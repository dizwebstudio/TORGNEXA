# ADR 0106: Bitrix24 CRM production runtime

Status: Accepted

## Context

Task 097 delivered a provider-neutral CRM SDK and a qualified Bitrix24
adapter, but the built-in production registry still treated the connector as
planned. The Settings catalog therefore could not create an account or run a
health check, even though the adapter already implemented universal
`crm.item.*` and `crm.item.productrow.*` operations.

## Decision

Admit Bitrix24 on a dedicated `crm` separate surface. The built-in registry
constructs the adapter only through the common pinned HTTPS transport and
resolves the lower-case `portal_host` from the tenant-scoped non-secret runtime
configuration store. The OAuth runtime supplies only the current access-token
bytes through the SDK `SecretAccessor`; refresh bundles and client material
remain host-owned.

The runtime advertises the four qualified CRM capabilities: entity read,
entity write, product-row read and product-row replacement. Account creation,
credential enrollment, OAuth sign-in/refresh, health checks and exact
capability selection use the existing connector-account control plane. CRM is
not admitted to generic product synchronization: the worker continues to
accept only the canonical commerce `products` bridge.

## Consequences

Bitrix24 can be configured and verified from the integration catalog and its
CRM adapter is reachable through a reviewed provider-neutral registry. The
card no longer claims a generic marketplace/product sync. Multifields,
activities, events, invoices and other deferred SDK surfaces remain
unadvertised until their own host workflows exist.

## Compatibility impact

Connector SDK v1 and existing public API paths remain unchanged. The runtime
support contract and generated TypeScript catalog add the existing `crm`
separate-surface literal and a non-secret configuration template. No existing
event or OpenAPI schema changes.

## Migration and data impact

No database migration or backfill is required. Existing connector-account and
runtime-config tables already support tenant-scoped CRM accounts. Operators
must save `portal_host` before the first health check.

## Security and privacy impact

Portal hosts are validated as lower-case public DNS names and are reached only
through the common HTTPS/SSRF-controlled transport. Access and refresh tokens
never enter URLs, request bodies, logs, events or normal database columns.
CRM writes remain capability-gated and carry the connector's idempotency key;
the adapter reconciles ambiguous outcomes before reporting success.

## Operational impact

The catalog inventory becomes 11 generic product integrations, 13 working
separate-surface providers and 15 planned connectors. A failed portal health
check produces the existing bounded account health state and never affects
other connectors or worker components. Live Bitrix24 qualification still
requires a dedicated non-production portal and OAuth client.

## Alternatives considered

Marking Bitrix24 as a generic `ready` integration was rejected because CRM
entities are not the commerce product entity and the worker has no CRM sync
bridge. Leaving it planned was rejected because it hid an already qualified
adapter and prevented account/OAuth/health operation. Adding a provider-name
branch to Core was rejected; all Bitrix24 protocol behavior remains inside the
built-in composition boundary and connector package.
