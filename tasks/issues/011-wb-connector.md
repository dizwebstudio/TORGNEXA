# Task 011

Create current official WB Connector Spec; implement health and selected read-only catalog/inventory with fixtures/sandbox. Writes separate.

## Status

Completed.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks.

## Repository completion — 2026-08-10

Repository implementation is complete.

- `wildberries` is the first active architecture-registered marketplace provider after Tasks `010`, `025`, `029`, and `064` established the SDK/security/sandbox/conformance prerequisites.
- The manifest is least-privilege and read-only: only `products.read` and `inventory.read`; bearer secrets remain behind Connector SDK `SecretAccessor`.
- Current official WB baseline is documented for Content `POST /content/v2/get/cards/list`, seller warehouses `GET /api/v3/warehouses`, stock reads `POST /api/v3/stocks/{warehouseId}`, domain `/ping`, current token categories and 2026 `chrtId` stock identity.
- Product pagination uses an opaque connector cursor over the official `updatedAt` + `nmID` cursor; WB `nmID`, `chrtID` and warehouse IDs remain remote identities and never enter Core models.
- Inventory reads use current `chrtId` requests, bounded batches and fail-closed validation for unexpected/duplicate IDs, negative quantities and malformed/oversized responses.
- Remote HTTP/transport failures are normalized to bounded Connector SDK errors; raw remote bodies, headers, token material and transport error text are not surfaced.
- Provider code imports only the approved `internal/platform/connectors` SDK prefix and has no direct `net/http`, DNS/socket, filesystem/process, SQL, Core or App authority.
- Deterministic synthetic fixtures cover product cards, cursor behavior, warehouse discovery, stocks and error normalization.
- The canonical Task-064 report at `docs/connectors/wildberries/conformance-report.json` passes all 13 mandatory checks, including Linux namespace/chroot sandbox isolation.
- ADR-0048 records the first provider-admission decision and keeps all WB write capabilities deferred to separately risk-reviewed tasks.

Operational note: repository-local architecture validation can recognize the existing prerequisite completion wording, but the trusted-base hosted workflow must merge that parser normalization before a subsequent protected provider-admission PR can prove the merge-base prerequisite rule. This does not change Task 011 code semantics; it is an external architecture-qualification sequencing requirement tracked with Task 080.
