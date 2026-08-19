# Task 078: Plugin marketplace governance

Status: **repository-complete** (2026-08-11)

## Objective
Implement plugin trust metadata, signing/checksum verification, requested capability consent and revocation model.

## Dependencies
025, 064, 065

## Repository implementation

- Added `internal/platform/pluginmarketplace` as a non-executing governance composition layer over Tasks 025/029/064/065.
- Marketplace listings expose immutable artifact digest, publisher/key identity, trust level, license, security contact, requested capabilities/secret classes/exact TLS destinations and resource ceilings before activation.
- Official/verified listings require complete conformance, supply-chain, malware, license/security-contact, SBOM and provenance review evidence; community/private still require conformance/supply-chain/malware/license/security-contact review.
- Exact tenant consent is bound to plugin id + semantic version + artifact SHA-256 and validated again by Task-025 `Prepare`; a new artifact never inherits a prior grant.
- `AssessUpdate` explicitly reports new capabilities, secret classes, network destinations, raised resource ceilings, publisher identity changes and trust downgrade. Any different artifact/version requires reapproval even if authority is unchanged.
- Added fail-closed append-only revocation for artifact digest, publisher key and exact tenant installation consent.
- Added tenant-private listing rules; private plugin metadata cannot cross organization/workspace scope.
- Added migration `000028_plugin_marketplace_governance.sql`, PostgreSQL evidence adapter, typed plugin marketplace contracts and OpenAPI `0.7.1` listing response.
- Added ADR `0051` and architecture review `ARCH-078`.

## Acceptance

- [x] Trust level is visible before installation.
- [x] Signed SHA-256/Ed25519 artifact identity is reverified at admission through Task 025.
- [x] Requested functional, secret, network and resource authority is visible before activation.
- [x] Every new artifact/version requires fresh exact-artifact consent; capability/secret/network/resource escalation is explicitly surfaced and cannot be auto-approved.
- [x] Artifact, publisher-key and tenant-installation revocations fail closed and preserve history.
- [x] Private plugins are tenant-isolated; marketplace records carry no credentials/private signing material.
- [x] Marketplace admission produces only the existing inert Task-025 plan for Task-029; no parallel execution path exists.

## Dependency boundary

Task `078` closes the Phase-2 plugin publication/admission governance chain. Provider connectors still require their normal architecture/conformance review and Social Core remains provider-neutral. The canonical next dependency-ready task is `040 VK Connector`, followed by `041 Telegram` and `042 MAX`.
