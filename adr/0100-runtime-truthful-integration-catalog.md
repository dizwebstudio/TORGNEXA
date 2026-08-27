# ADR 0100: Runtime-truthful integration catalog

Status: Accepted

## Context

TORGNEXA has 38 admitted connector manifests, but a manifest proves an SDK
implementation and capability declaration rather than end-to-end production
execution. The generic worker originally resolved product readers for six
connectors, product writes for WooCommerce, price writers for Yandex Market and
WooCommerce, and only the canonical product entity. Projecting all manifests as
equally connectable caused the product catalog to promise operations for which
the application had no host transport, domain bridge or worker route.

Inferring readiness from the presence of a provider package or from manifest
capabilities is unsafe. Those facts cannot prove that credentials, non-secret
configuration, host-mediated egress, canonical mapping and worker dispatch are
composed in the production binary.

## Decision

Add a reviewed, versioned built-in runtime-support contract covering every
catalog connector. Each entry has one stage (`ready`, `separate_surface` or
`planned`), an owning product surface, exact operational capabilities and exact
entity/direction pairs. Generation fails unless the contract and manifest
inventory have identical connector IDs and every operational capability is a
subset of its manifest.

The contract generates both the frontend catalog projection and the Go runtime
admission table. API account creation, capability updates, enablement and sync
policy authorization consult the Go table and fail closed. Settings presents
planned entries as unavailable and sends AI connectors to their existing
dedicated settings surface. The worker keeps its independent product-entity
check.

The built-in composition boundary from ADR 0090 is extended with already
admitted AliExpress RU, Magnit Market, Megamarket, OpenCart and PrestaShop
adapters. This raises generic product runtime coverage from six to eleven
connectors. It does not claim support for any other manifest capability.

## Consequences

The catalog remains useful for discovery without manufacturing availability.
New provider code cannot silently become a product promise; readiness requires
an explicit reviewed contract change and executable bridge tests. Conversely,
adding a bridge requires updating one source of truth or generation fails.

Existing accounts for connectors that are now classified as planned remain
stored and may be disabled or have legacy OAuth material rotated, but cannot be
newly created, enabled, health-approved or synchronized through the generic
surface. This preserves recovery paths without granting non-executable runtime
authority.

## Compatibility impact

Connector SDK v1, manifests and event schemas are unchanged. Existing REST
operation shapes are unchanged; unsupported requests now return the already
used HTTP 422 class instead of allowing an account or policy that cannot run.
The TypeScript catalog projection gains additive runtime metadata.

## Migration and data impact

No database migration is required. Runtime support is immutable build-time
metadata. Existing tenant rows are not rewritten or deleted.

## Security and privacy impact

Default deny is applied before credential use or remote egress. Runtime
configuration templates contain only non-secret fields and continue through
the existing secret-key rejection and tenant-scoped storage. Provider HTTP
calls retain ADR 0090 host, DNS, TLS and redirect controls. No PII or credential
material is added to the contract or generated frontend data.

## Operational impact

Operators see 11 generic integrations that can run, six AI providers in their
own settings surface and 21 explicitly planned entries. Config-bearing ready
connectors must save and validate their non-secret runtime configuration before
enablement. Health checks now invoke the concrete built-in adapter rather than
a manifest-only probe.

## Alternatives considered

Hiding all non-runtime manifests was rejected because the product still needs a
roadmap/discovery catalog. Treating every SDK connector as ready was rejected
because it recreates the defect. Implementing provider switches in the worker
was rejected by ADR 0090; composition remains isolated in the single audited
built-in runtime module.
