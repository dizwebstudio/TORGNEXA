# Task 035: Magnit Market connector

## Objective
Create current capability audit/spec and implement supported baseline connector methods.

## Dependencies
010, 014

## Repository implementation
**Completed 2026-08-10.**

Task 035 registers `magnit-market` as a read-only marketplace provider through Connector SDK v1. The baseline uses the current official Magnit Market Partner API with `X-Api-Key` authentication and grants only `products.read`, `prices.read`, `inventory.read`, and `orders.read`.

## Deliverables
- `connectors/marketplaces/magnit-market`: host-mediated connector, fixtures and adversarial tests;
- `docs/connectors/magnit-market`: current capability audit, connector spec, reconciliation notes, conformance plan and immutable Task-064 report;
- architecture provider admission `ARCH-035`;
- explicit unsupported/write capability boundary.

## Acceptance evidence
- product reads are scoped to the configured shop and map one bounded projection per remote SKU;
- product observation timestamps are joined from the official price-info response rather than synthesized locally;
- prices preserve exact decimal JSON spelling and use shop-scoped keyset pagination;
- inventory exposes only the aggregate stock identity actually present in the audited read API; no physical warehouse identity is invented;
- orders bind the required `created_at` window into the opaque pagination cursor and exclude buyer/delivery-region data from the normalized projection;
- all mutation capabilities remain denied;
- API keys are callback-scoped through Task 021 and provider code has no direct network/database/filesystem/process/Core/App authority;
- provider-specific Task-064 conformance passes all thirteen mandatory checks.

## Remaining qualification
Live staging evidence with an actual Magnit Market seller shop/API key, observed rate ceilings, stock-type topology and production-shaped payloads remains deployment qualification and is not represented by synthetic fixtures.

## Follow-ups
Canonical dependency order continues at Task `018`; the optional extended-marketplace branch continues with Task `036` AliExpress RU.
