# Task 130: Runtime-truthful integration catalog

## Status

`complete` — 2026-08-26. The generic product runtime, API admission and catalog
UI now share one exact support contract; validation results are recorded below.

## Objective

Stop the 38-entry connector inventory from claiming end-to-end availability
that the production application cannot execute, while activating the existing
product-capable built-in adapters that already satisfy the connector boundary.

## Deliverables

- [x] add a schema-backed runtime-support declaration with exact stage, surface,
  capability, entity and direction for all 38 connector manifests;
- [x] generate frontend and Go projections from that declaration and reject
  manifest/runtime inventory drift;
- [x] connect AliExpress RU, Magnit Market, Megamarket, OpenCart and PrestaShop
  product adapters through the ADR-0090 built-in runtime;
- [x] expose concrete health checks and non-secret runtime configuration for
  every configuration-bearing ready connector;
- [x] reject unsupported account creation, capability enablement and sync
  policy authorization at the API boundary;
- [x] mark planned cards as unavailable, route AI connectors to their dedicated
  settings surface and show only executable operations as working;
- [x] document the exact 11 ready / 6 separate / 21 planned runtime state.

## Safety invariants

- a connector manifest is never treated as proof of runtime execution;
- operational capabilities must be a subset of the canonical manifest;
- both the API and worker reject unsupported entity/direction combinations;
- provider implementations remain behind the single audited built-in
  composition boundary and use host-mediated network access;
- credentials remain in SecretProvider; templates and generated catalog data
  contain only non-secret configuration;
- existing planned-connector rows are not deleted and cannot regain runtime
  authority merely by remaining in the database.

## Acceptance

- the generated catalog contains exactly the 38 canonical manifest IDs;
- all 11 ready integrations resolve a product reader and planned integrations
  fail closed;
- only OpenCart and WooCommerce advertise outbound product synchronization;
- frontend tests/typecheck/build distinguish ready, separate and planned cards;
- Go test/vet, contract, architecture and generation gates pass;
- API, worker and frontend images are rebuilt and recreated successfully.

## Validation

- full Go 1.26.7 test and vet suites: PASS;
- connector/runtime/API tests: PASS;
- contract/schema, generated SDK drift/runtime and architecture gates: PASS;
- frontend logic tests, TypeScript typecheck, static policy and production build: PASS;
- API, worker and frontend images rebuilt and containers recreated: PASS;
- API and frontend report healthy; worker reports all six components ready.
