# Task 082: Product Compliance Core

## Status
Repository-complete. Runtime PostgreSQL/official-registry/Notification Center qualification remains environment/integration evidence, not missing repository implementation.

## Objective
Implement Product Compliance document registry and explainable policy engine for certificates/declarations/EAC/other evidence with GTIN/SKU/category scoping and expiry/revocation handling.

## Dependencies
004, 017, 023, 060, 081

## Deliverables
Domain/migrations/API/policy evaluation/expiry notifications/registry-verification port/publication guard; contracts/events/tests.

## Acceptance
A product/listing with missing/expired required evidence can be blocked before connector write; reasons and evidence are auditable; no connector bypass.

Run required repository checks and report results, risks and follow-ups.
