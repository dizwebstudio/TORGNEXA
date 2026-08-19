# Task 036: AliExpress RU connector

## Objective
Audit current Russia-facing seller integration path and implement supported baseline connector methods.

## Dependencies
010, 014

## Repository implementation
**Completed 2026-08-10.**

Task 036 registers `aliexpress-ru` as a deliberately narrow read-only marketplace provider. Admission is based on the Russia-facing seller API (`openapi.aliexpress.ru`, JWT in `X-Auth-Token`) and grants only `products.read`; it does not import global AliExpress API assumptions.

## Deliverables
- `connectors/aliexpress-ru`: host-mediated Russia seller product reader, cursor, fixtures, adversarial tests and Task-064 conformance candidate;
- `docs/connectors/aliexpress-ru`: capability audit, spec, reconciliation notes, conformance plan/report;
- architecture provider admission `ARCH-036`;
- explicit deferred capability boundary for inventory, prices, orders and every mutation.

## Acceptance evidence
- product reads use the Russia-specific filtered scroll contract with bounded `last_product_id` pagination;
- product/variant/SKU identifiers remain remote mapping keys outside Core;
- `ali_updated_at` is preserved as the UTC remote observation timestamp and local `now()` is never substituted;
- legacy/deprecated product stock fields are ignored and cannot authorize inventory synchronization;
- JWT material exists only inside Task 021 callbacks and provider code has no direct network/database/filesystem/process/Core/App authority;
- remote errors/bodies are normalized and bounded;
- provider-specific Task-064 conformance passes all thirteen mandatory checks.

## Remaining qualification
Live Russia seller-account staging evidence is required before admitting current stock, price or order read contracts. This is intentionally represented as deferred capability admission rather than guessed from global AliExpress documentation.

## Follow-ups
Canonical dependency order remains Task `018`; the optional extended-channel branch continues with Task `037` Avito.
