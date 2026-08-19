# ADR 0037: Isolated third-party plugin security boundary

Status: Accepted

## Context

Connector SDK v1 is implemented, but accepting third-party code before an explicit isolation contract would let provider packages inherit ambient process authority such as host networking, filesystem access, environment variables, child-process execution, Core imports, or direct secret persistence. ADR-0020 requires signed/checksummed plugins, declared permissions and explicit consent, while ADR-0036 deliberately left arbitrary third-party execution disabled until Task 025. The boundary must be stable before Task 029 implements sandbox/dry-run behavior and before Task 064 can certify providers against it.

## Decision

Define a non-executing security boundary in `internal/platform/pluginsecurity`. A future isolated plugin is described by a signed descriptor that embeds the stable Connector SDK v1 manifest, an immutable SHA-256 artifact identity, publisher/key identity, trust level, requested functional capabilities, requested secret classes, exact TLS egress destinations, and bounded resource ceilings. Publisher signatures use Ed25519 over a deterministic message that binds the canonical manifest, artifact digest/size, publisher/key identity, trust level, requested permissions and isolation limits.

Installation grants are separate least-privilege records bound to the exact connector id, semantic version and artifact digest. A grant may only be a subset of the signed request; an artifact update never inherits a previous grant implicitly. The host verifies artifact bytes, digest, publisher key fingerprint and signature before producing an inert `AdmissionPlan`. Task 025 intentionally provides no launcher, dynamic loader, WASM interpreter, command/environment/filesystem field, or arbitrary code-execution API.

Strengthen the architecture checker so provider implementations cannot directly import process/plugin/syscall/unsafe APIs, host filesystem/environment APIs, or direct network packages. Provider code remains restricted to `internal/platform/connectors`; network and execution authority must later be mediated by a host-owned sandbox boundary. Provider admission remains disabled until Tasks 029 and 064 complete.

## Consequences

Connector SDK v1 can now be treated as dependency-stable: its root Connector, Runtime and SecretAccessor surfaces are frozen by regression tests, and third-party isolation has a versioned serialized contract without admitting code. Future plugin artifacts can be verified and permission-reviewed before any runtime exists. Task 029 can implement sandbox enforcement against explicit ceilings rather than inventing a permission model, and Task 064 can test conformance against stable security expectations.

The least-privilege model is intentionally stricter than current in-process library conventions. Third-party providers must not assume direct sockets, filesystem, environment or child processes. Any capability genuinely required later must be added as a generic host-mediated SDK capability with a new compatibility/security review rather than as ambient authority.

## Alternatives considered

Loading third-party Go plugins in-process with `plugin.Open`, shared libraries, reflection or arbitrary `os/exec` was rejected because it grants the host process authority and makes tenant/secret/network isolation non-auditable. Allowing providers to call `net/http` directly was rejected because an allowlist in documentation cannot prevent SSRF, DNS rebinding or access to internal endpoints. Granting the entire signed manifest automatically was rejected because installation consent must be able to narrow capabilities and network/secret access. Persisting command lines, environment or filesystem mounts in the Task-025 plan was rejected because those are execution semantics owned by the later sandbox task.

## Compatibility impact

Connector SDK major remains `1`; existing Task-010 manifest-v2, Account, Health, RemoteError, Runtime and SecretAccessor contracts are not replaced. The Task-025 descriptor/grant/admission schemas are additive under `contracts/plugins/*-v1.schema.json`. SDK v1 root interface method counts are frozen by tests so adding a method becomes an explicit major-version decision instead of an accidental source break.

Official in-tree connectors remain possible under the existing source architecture, but future third-party provider admission must satisfy the new security descriptor and grant boundary in addition to existing manifest/capability requirements. Provider admission remains disabled, so this task does not change production provider behavior.

## Migration and data impact

Task 025 introduces no database migration and stores no new runtime state. Permission grants are defined as a contract/value object only; persistence and installation marketplace lifecycle can be added by the owning governance tasks without changing the security semantics. Existing connector account and secret-reference tables are untouched.

Artifact updates are intentionally modeled as new immutable digest identities. A later persistent grant store must key grants to connector id, version and artifact digest and must not migrate grants to a new digest automatically.

## Security and privacy impact

The boundary verifies exact artifact SHA-256, Ed25519 publisher signatures, trusted public-key fingerprints and least-privilege grant subsets before returning an inert plan. Requested secret classes must be declared by the Connector SDK manifest; raw secret bytes are absent. Network requests are exact lower-case DNS host plus port entries; wildcard, IP-literal, localhost and local/single-label targets are rejected at the contract layer. Task 029 must still enforce DNS resolution/rebinding and actual egress at runtime.

The architecture checker blocks provider direct imports of `os/exec`, `plugin`, `syscall`, `unsafe`, host filesystem/environment packages and direct `net` packages. This prevents a future admitted source provider from bypassing the SDK boundary even before process isolation is implemented. No PII or credential payload is added to the descriptor, grant or admission plan.

## Operational impact

There is still no third-party plugin launcher. Operators may verify a candidate artifact and permission request, but they cannot activate arbitrary code from Task 025 alone. Provider admission stays fail-closed until Task 029 implements sandbox/dry-run and Task 064 proves conformance; Task 080 hosting qualification remains independently required.

Task 029 must map `IsolationLimits`, exact network destinations and granted secret/capability scopes into enforceable runtime controls and must prove production credential isolation. Task 064 must include negative tests for permission escalation, direct-host access, signature/digest mismatch and SDK-boundary bypass.
