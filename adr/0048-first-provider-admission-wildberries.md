# ADR 0048: Open provider admission with Wildberries as the first read-only reference

Status: Accepted

## Context

Tasks 010, 025, 029 and 064 are repository-complete and already establish the frozen Connector SDK v1 contract, signed least-privilege plugin boundary, executable sandbox/dry-run isolation and machine-verifiable conformance suite. Architecture policy intentionally kept provider admission disabled in each prerequisite task so no task could both create and waive its own prerequisite. Task 014 now provides provider-neutral reconciliation and the execution plan calls for Wildberries as the first real marketplace reference.

Task 011 must therefore do two things in one reviewed change: open the previously prepared admission control and register one concrete provider. Leaving admission disabled while placing executable code under a provider root would violate the architecture lifecycle gate; bypassing the provider root would violate provider-specific code isolation.

## Decision

Enable `provider_admission` only after the four prerequisite tasks are already complete and register exactly one active provider: `wildberries`, family `marketplace`, under `connectors/marketplaces/wildberries`.

The admitted provider is read-only and declares only `products.read` and `inventory.read`. Its Go package may import the existing `internal/platform/connectors` SDK prefix and approved standard library only. It may not import Core, App, PostgreSQL, filesystem/process/network primitives or other provider packages. Network execution remains a host-injected transport under Task-025/029 controls; provider code does not import `net/http`.

Task 011 also adds additive SDK-v1 capability-specific read interfaces for product and inventory projections. These are provider-neutral and carry only remote identities/bounded values. They do not change the frozen root `Connector` or `Runtime` interfaces and therefore do not require SDK major 2.

## Consequences

Wildberries becomes the first architecture-registered provider and must retain canonical manifest/spec/capability-audit/conformance evidence. Future providers must follow the same route and cannot rely on this decision as blanket approval: each provider still needs its own current official API audit, architecture review and passing conformance report.

Opening admission increases the importance of the protected trusted-base architecture workflow. Repository validation can verify the policy and evidence, but production release still requires the external protected-branch/ruleset qualification already tracked by Task 080/065 operational evidence.

## Alternatives considered

Keeping provider admission disabled and placing Wildberries logic inside `internal/platform` was rejected because it would make provider-specific branching part of the host architecture and bypass lifecycle controls.

Adding `net/http` to the provider allowlist was rejected because provider code must not gain direct socket/DNS authority. A host-injected bounded transport preserves the isolation model.

Adding WB-specific methods or fields to Core Product/Inventory models was rejected because `nmID`, `chrtID` and warehouse identifiers are remote identities owned by Connector SDK mappings and reconciliation.

Enabling write capabilities in the first provider was rejected. Product, price, stock and order mutations need separate risk/compliance/idempotency/dry-run evidence and are intentionally outside Task 011.

## Compatibility impact

`Connector` and `Runtime` SDK v1 root interfaces are unchanged. `ProductReader` and `InventoryReader` are additive capability-specific interfaces. Existing connectors/providers do not exist in the active inventory before this change and therefore no implementation is broken.

## Migration and data impact

No database migration is introduced. Persistent local/remote identity continues to use existing connector-account/entity-mapping tables and Task-013/014 state/evidence.

## Security and privacy impact

The provider holds no database or durable secret authority. Tokens exist only in the existing secret callback and synchronous transport call. Raw remote bodies/errors are not emitted as SDK errors. Production egress remains exact-host TLS only and subject to Task-029 DNS/rebinding/isolation enforcement. Fixtures contain synthetic data only.

## Operational impact

Before each connector release, official WB documentation/release notes must be re-audited because token categories, fields and request limits can change independently. Task-064 conformance and provider semantic tests are release blockers. Live seller qualification uses least-privilege test credentials and must not persist business payloads as conformance evidence.
