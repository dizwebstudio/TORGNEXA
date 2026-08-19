# ADR 0051: Plugin marketplace governance and exact-artifact consent

Status: Accepted

## Context

Tasks 025, 029, 064 and 065 establish the signed plugin security descriptor, isolated runtime, conformance gate and release/supply-chain evidence. They do not define how reviewed plugin versions are distributed, how users see trust/authority before installation, how consent behaves across updates, or how compromised artifacts, publisher keys and individual installations are revoked.

A marketplace that merely downloads signed bytes is insufficient: publisher identity is not equivalent to approval, a signature does not authorize requested capabilities, and an update must not silently expand functional, secret, network or resource authority.

## Decision

Introduce `internal/platform/pluginmarketplace` as the governance composition layer. It does not execute plugins. A marketplace listing is an immutable reviewed version that embeds the signed Task-025 descriptor and adds license/security-contact/review evidence. The user-facing listing projection always exposes publisher, immutable artifact digest, trust level (`official`, `verified`, `community`, `private`), requested capabilities, secret classes, exact TLS destinations and isolation ceilings before activation.

Every installation uses an explicit tenant consent containing a Task-025 `PermissionGrant` bound to the exact plugin id, semantic version and SHA-256 artifact digest. A different version or digest never inherits an old consent. `AssessUpdate` additionally reports privilege growth across capabilities, secret classes, network destinations and resource ceilings, plus publisher-identity changes or trust downgrade, so escalation is visible rather than inferred.

Admission is a composition, not a new runtime: reviewed listing -> tenant consent -> revocation check -> Task-025 SHA-256/Ed25519/request-vs-grant verification -> inert `AdmissionPlan` -> Task-029 sandbox. Task-064 conformance and Task-065 supply-chain/malware/license evidence are review inputs. Official/verified artifacts additionally require subject-bound SBOM and provenance evidence.

Revocation is append-only and fail-closed at three scopes: global artifact digest, global publisher key, and tenant installation consent. A revocation never deletes marketplace/consent history. Private plugin versions are tenant-scoped and cannot be discovered or installed cross-tenant.

## Consequences

Plugin trust is visible and separate from cryptographic authenticity. A verified signature cannot bypass marketplace review or tenant consent. Updates are intentionally noisier than auto-upgrade systems because new bytes always require fresh exact-artifact consent; privilege escalation is separately highlighted for the approver.

Marketplace governance stores metadata/evidence only. It receives no private signing key, provider credential, filesystem/process authority or direct socket capability. Runtime secrets remain Task-024/029 concerns.

The public `/plugins` response becomes a typed marketplace listing page and OpenAPI advances additively to `0.7.1`; operation count remains unchanged.

## Alternatives considered

Automatically carrying grants to a new digest was rejected because it contradicts the Task-025 immutable artifact boundary and makes compromised/update bytes inherit authority. Treating a valid publisher signature as installation approval was rejected because authenticity is not authorization. Mutable `revoked=true` flags were rejected because they erase evidence chronology. A single global table mixing private and public listings under conditional RLS was rejected in favor of separate global/private storage with simpler fail-closed tenancy rules.

## Compatibility impact

Connector SDK v1 interfaces and capability vocabulary are unchanged. Existing in-tree official connectors are unaffected. OpenAPI changes only the response schema/documentation of the already-existing `listPlugins` operation; generated SDK operation inventory remains 30.

## Migration and data impact

Migration `000028_plugin_marketplace_governance.sql` adds immutable public/private listing, consent and revocation evidence. Public marketplace versions and global security revocations are global control-plane facts; private versions, consents and installation revocations use explicit organization/workspace keys and forced RLS.

## Security and privacy impact

Private packages are tenant-isolated. Credentials/private signing material are never marketplace fields. Exact digest/signature verification is repeated during admission. Global artifact/key revocations and tenant installation revocations block admission before Task-029 runtime creation. Trust downgrade, new capability, secret, network destination or increased ceiling is surfaced as escalation requiring reapproval; in practice every new artifact requires fresh consent even without escalation.

## Operational impact

Marketplace operators must maintain review evidence, security contacts and revocation feeds. Official/verified publication requires Task-065-grade SBOM/provenance evidence. Emergency response can revoke one artifact or a compromised publisher key globally without deleting prior installation evidence; tenant admins can revoke only their own installation consent.
