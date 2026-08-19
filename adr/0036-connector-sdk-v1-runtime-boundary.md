# ADR 0036: Connector SDK v1 runtime and account boundary

Status: Accepted

Supersedes and extends the runtime/account portions of ADR-0010 without modifying the accepted historical ADR.

## Context

ADR-0010 established capability declarations as the provider-neutral connector contract, but the executable repository still lacked a stable SDK manifest, account lifecycle, scoped secret access, normalized health/error semantics, and a host boundary that future isolated providers can use without importing Core or persistence internals. The backlog also now contains first-class FX and notification providers, so the family vocabulary must cover those capabilities without provider-specific exceptions. Provider admission must remain disabled until the separate plugin-isolation, dry-run and conformance tasks are complete.

## Decision

Define Connector SDK major v1 in `internal/platform/connectors`. A strict additive manifest-v2 declares one canonical family, semantic connector version, SDK major, capabilities, non-secret authentication requirements, and bounded rate-limit/retry policy. The executable Go capability registry must remain in lockstep with `contracts/connector-capabilities.yaml`, and capabilities must be valid for the declared family.

Connector accounts are tenant-owned bindings to a stable manifest identifier. Credentials are represented only by opaque `sec:v1:...` references. Provider-facing code receives callback-only secret access through the SDK Runtime interface; the host-only `internal/platform/connectorruntime` adapter bridges that interface to Task-021 SecretProvider and tenant scope. Durable health and remote failure state contains only bounded normalized machine categories/codes, never raw remote response bodies or raw secret-bearing errors.

Register host-side connector runtime and PostgreSQL account repository modules inside the existing `connector-plugin-runtime-capabilities` frozen pillar. Provider implementations remain outside the repository inventory: `provider_admission.enabled` stays false and its prerequisite list remains Tasks 010, 025, 029 and 064. The SDK deliberately provides no universal untyped Invoke method; concrete domain operations remain typed capability interfaces owned by their connector/domain tasks.

## Consequences

Future providers get one narrow import boundary and stable account/secret/error semantics without gaining access to Core, PostgreSQL, Kafka, or concrete secret internals. Connector account version and health state become safely persistent and optimistically updated. FX and Notification become first-class connector families instead of special cases. The additional manifest-v2 is additive, so the older manifest contract remains available for compatibility. Provider execution still cannot be admitted until Task 025 isolation, Task 029 dry-run/sandbox and Task 064 conformance close.

## Alternatives considered

Allowing each connector to import shared Core, HTTP, PostgreSQL and secret packages directly was rejected because it would make Task 025 isolation expensive and would permit provider-specific architecture drift. A generic `Invoke(operation string, payload any)` API was rejected because it would erase compile-time capability boundaries and invite unversioned provider-specific payloads. Renaming the existing `connector_accounts.provider` column immediately was rejected because a destructive rename would violate the expand-first migration policy; it remains the physical durable manifest-id column until a later contract migration is safe.

## Compatibility impact

The SDK contracts are additive. `contracts/plugins/manifest.schema.json` remains unchanged and a separate `manifest-v2.schema.json` introduces the stronger v1 runtime contract. Existing public API and event types are unchanged. The existing physical `connector_accounts.provider` column remains in place and is interpreted as the canonical connector manifest ID, preserving old readers/writers during the expand migration.

## Migration and data impact

Migration `000012_connector_sdk.sql` adds account version, update time and normalized health metadata with compatibility defaults, extends canonical family constraints to FX and Notification, constrains the durable manifest ID, and adds lifecycle/version/immutability guards. Tenant RLS remains forced. Application hard delete and TRUNCATE are blocked. No plaintext credential column is introduced; accounts continue to store only Task-021 opaque secret references.

## Security and privacy impact

Provider-facing packages can import only the connector SDK boundary. Secret material is available only through a scoped callback and is not part of account DTOs, manifests, health records or normalized errors. Arbitrary provider errors are converted to bounded reason codes before persistence, reducing credential and PII leakage. Provider admission remains fail-closed, so this ADR does not authorize arbitrary third-party code execution or sensitive provider writes.

## Operational impact

Hosts must register manifests before binding accounts and must supply a tenant-scoped secret runtime. Health checks and retries operate on normalized bounded metadata and optimistic account versions. Exact provider execution, isolation, sandbox/dry-run and conformance qualification remain later tasks. Deployment CI must apply migration 000012, run tenant/RLS rehearsal, and preserve the external Task-080 protected-workflow qualification before claiming hosted architecture readiness.
