# Plugin & Connector SDK

Task 010 defines Connector SDK major `1`. The SDK is provider-neutral and is the
only internal package a provider implementation is allowed to import directly:

`internal/platform/connectors`

The host may adapt secrets, transport, HTTP, metrics and other platform services
behind the narrow SDK runtime surface. Providers do not import Core, PostgreSQL,
Kafka, the concrete SecretProvider implementation, or other platform internals.

## Families

SDK v1 has eleven first-class families:

- Marketplace
- Classified
- Social
- ERP
- EDO
- Government
- Payment
- Logistics
- PickupPoint
- FX
- Notification

FX and Notification are explicit families rather than exceptions hidden behind
provider names. This reconciles the earlier nine-family manifest vocabulary with
`fx.rates.read` and `notifications.sms.*` capabilities. Tasks 089 and 091 still
own the domain-specific FX and SMS provider contracts/reference behavior.

## Manifest v2 / SDK v1

New connectors use `contracts/plugins/manifest-v2.schema.json`. The earlier
`manifest.schema.json` remains unchanged for compatibility; it is not the
stabilized SDK-v1 manifest.

Every manifest declares:

- stable connector id;
- display name;
- family;
- connector semantic version;
- `sdk_version: 1`;
- unique capabilities from `contracts/connector-capabilities.yaml`;
- non-secret auth requirements;
- static rate-limit/timeout/retry policy.

The executable Go capability registry is checked against
`connector-capabilities.yaml` so contract/code vocabulary drift fails tests.
Family/capability compatibility is enforced by the SDK registry at runtime.

Core and workflows check capabilities (`products.read`, `prices.write`,
`social.post.media`, `edo.documents.send`, etc.), never provider names.

## Account model

`connector_accounts.provider` is retained as an expand-compatible database
column but its Task-010 meaning is the canonical manifest `connector_id`.
Renaming the physical column is intentionally deferred to a future contract
migration so old readers/writers remain compatible.

An account contains tenant identity, connector/family binding, lifecycle,
optimistic version, normalized health and an optional opaque Task-021
`sec:v1:...` reference. Provider passwords/tokens/client secrets are forbidden
in the account table and SDK DTOs.

New accounts start `disabled`, version `1`, health `unknown`. Account identity,
connector id and family are immutable. Health/status updates are optimistic and
hard delete/truncate is blocked.

## Secret access

Provider code receives only `connectors.SecretAccessor`. Plaintext exists only
inside `UseSecret(...)` callback scope. `internal/platform/connectorruntime`
binds that SDK callback to a tenant-scoped Task-021 `SecretProvider`; provider
code never receives the secret repository/master-key source.

## Health

Health is normalized to:

- `unknown`
- `healthy`
- `degraded`
- `unavailable`

Only a bounded machine `reason_code` may be stored. Raw HTTP bodies, URLs,
headers, tokens and arbitrary provider error text are forbidden. A host manager
discards unnormalized error strings and records `connector_health_failed`.

## Normalized remote errors

Provider adapters return an SDK `RemoteError` category and machine code:

- invalid_request
- unauthorized
- forbidden
- not_found
- conflict
- rate_limited
- transient
- unavailable
- timeout
- unsupported
- internal

Only retryable categories can drive host retries. Provider `Retry-After` is
bounded by the manifest retry policy; otherwise deterministic exponential
backoff is used. Raw provider error text is not part of this contract.

## Capability-specific operations

The root `Connector` interface intentionally contains only `Manifest` and
`Health`. Domain operation interfaces are added as typed capability-specific
contracts by their owning tasks/reference connectors. SDK v1 deliberately has
no universal `Invoke(map[string]any)` escape hatch.

## Isolation and admission

Task `025` stabilizes SDK v1 by defining the isolated third-party security
boundary without enabling arbitrary execution. A third-party package must carry
a signed `security-descriptor-v1` that binds its canonical Connector SDK v1
manifest, SHA-256 artifact identity, publisher key identity, requested
capabilities, secret classes, exact TLS egress destinations, and resource
ceilings. Installation consent is represented by a separate grant bound to the
exact connector version and artifact digest; grants may only narrow the signed
request.

`internal/platform/pluginsecurity` verifies artifact bytes, Ed25519 signature,
publisher-key fingerprint and least-privilege grant, then returns an **inert**
`AdmissionPlan`. Task 025 contains no process launcher, dynamic loader, command,
environment, filesystem mount or raw-secret field. The architecture gate also
forbids provider packages from directly importing process/plugin/syscall/unsafe,
host filesystem/environment, or network packages.

SDK major `1` root interfaces (`Connector`, `Runtime`, `SecretAccessor`) are now
frozen by regression tests. Adding methods to those interfaces is a major-version
decision; capability-specific contracts must be additive interfaces rather than
mutating the root surface.

Task `029` now maps the signed limits/grants into the host-owned dry-run/test runtime in `internal/platform/connectorsandbox`. Dry-run never invokes a secret provider or network transport; test mode accepts only sandbox-tier credentials; exact granted egress is DNS-checked and IP-pinned by the host; and the Linux reference runner proves environment/filesystem/direct-network isolation plus resource ceilings with a deterministic emulator.

Task `064` now provides the mandatory machine-readable conformance harness across the stabilized SDK + Task-025 security boundary + Task-029 sandbox runtime. Provider admission remains fail-closed in the Task-064 completion change because Task 080 requires all admission prerequisites, including `064`, to already be completed in the merge base before a later protected change may enable admission.

## Task 078 marketplace admission

The signed Task-025 descriptor is now distributed through `internal/platform/pluginmarketplace`. Marketplace trust is presentation/review metadata, not runtime authority: installation still requires a tenant `PermissionGrant` bound to the exact plugin version and SHA-256 digest. A different artifact always needs fresh consent, while added capabilities, secret classes, exact TLS destinations or increased resource ceilings are separately surfaced as privilege escalation. Artifact/key/installation revocations are checked before `pluginsecurity.Prepare`; the resulting object remains the same inert `AdmissionPlan` consumed by Task 029.
