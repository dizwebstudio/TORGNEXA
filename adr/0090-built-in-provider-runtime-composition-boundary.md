# ADR-0090: Built-in provider runtime composition boundary

Status: Accepted

## Context

Connector SDK v1 deliberately prevents App, Core and ordinary Platform packages from importing concrete provider implementations. That rule stopped provider-specific branching from leaking into business logic, but after the production worker was composed it also left no audited place where qualified first-party connectors could be statically linked into the executable runtime. Keeping the worker provider-neutral while adding ad-hoc imports in `internal/app/worker` would violate the frozen connector/plugin boundary. Weakening the general import rule would be worse because every App/Platform package could then become a provider dispatch point.

TORGNEXA now needs production reconciliation for a bounded set of in-tree, already admitted and conformance-qualified connectors. Their packages continue to receive only Connector SDK interfaces and cannot import Core, PostgreSQL, App, filesystem, process or direct network authority. Host-owned HTTP/secret/configuration mediation must remain outside provider packages.

## Decision

Introduce exactly one policy-declared built-in composition module: `internal/platform/builtinruntime`. While provider admission is enabled, architecture policy requires this exact module to be registered as an infrastructure adapter. The architecture checker grants only this module permission to import concrete implementations that are already present in the active provider inventory. Imports of unregistered provider implementations remain forbidden.

The composition module may contain provider identifiers and construction dispatch because that is its sole purpose. App, Core and every other Platform package remain subject to the existing provider-specific identifier, literal, branch and table-dispatch prohibitions. Executables still import only registered internal runtime packages. Provider implementations still import only the Connector SDK-approved internal surface and therefore cannot import this host composition module or gain direct network/database authority.

`internal/platform/builtinruntime` owns the concrete first-party constructors and host-side typed HTTP adapters. It exports a narrow provider-neutral product reader/writer registry to the worker. Tenant-scoped non-secret account configuration is loaded by a callback supplied by the worker; credentials continue to resolve only through `sdk.Runtime`/`SecretProvider`. This decision does not create a generic untyped invoke surface and does not authorize third-party dynamic loading.

## Consequences

Built-in provider execution now has one explicit, testable composition point instead of hidden provider branching in the worker. The tradeoff is that this module becomes a small trusted computing-base surface: every change to its provider imports or the policy exception must pass the self-protected architecture gate and a pillar-change review. App/Core remain easier to reason about, while third-party providers still require the separate sandbox/artifact path rather than receiving this static-link privilege.

## Compatibility impact

The Connector SDK v1 interfaces and provider manifests are unchanged. Existing provider packages require no import changes. App/Core dependency direction remains unchanged except that App may consume the new ordinary internal infrastructure adapter through its provider-neutral API. The policy change is additive and keeps all previous provider-specific checks active outside the single composition module.

## Migration and data impact

No database schema is required by the composition boundary itself. Task 114 separately adds migration `000068` for versioned, tenant-scoped non-secret runtime configuration used by connectors that need host/store identifiers. Credentials remain absent from that table.

## Security and privacy impact

The exception is narrower than allowing provider imports generally: one exact module path is policy-pinned, registered, reviewed and checked against the active provider inventory. Unregistered provider imports are rejected. Provider packages still lack direct `net`, filesystem, process, database, Core and App authority. The host HTTP transport is HTTPS-only, DNS-pinned, redirect-disabled and rejects non-public destinations. Secret material remains callback-scoped and is never persisted in runtime configuration.

## Operational impact

The worker can now resolve admitted built-in connectors without provider branching in App/Core. Adding a future built-in provider still requires normal provider inventory/conformance evidence; the composition module cannot import a provider absent from policy. Third-party artifact execution remains governed by the existing plugin-security/sandbox decisions and is not broadened by this ADR. Rolling deployments may keep the worker dormant until the additive runtime migrations are present; missing Task-113 dispatch schema is treated as a temporary unavailable feature rather than a process-fatal error.

## Alternatives considered

Allowing `internal/app/worker` to import each provider was rejected because it directly violates the existing App boundary and makes the worker a growing provider switch. Allowing all infrastructure adapters to import providers was rejected because it materially weakens the architecture gate. Dynamic Go plugins were rejected because the repository explicitly forbids that execution model. Building an out-of-process provider host for every first-party connector would preserve the boundary but adds a larger protocol/artifact lifecycle than is necessary for the current qualified in-tree connectors; it remains an option for third-party isolation rather than a prerequisite for built-in execution.
