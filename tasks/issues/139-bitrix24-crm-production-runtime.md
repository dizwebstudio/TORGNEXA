# Task 139 — Bitrix24 CRM production runtime

## Status

`repository-complete` — 2026-08-28.

## Objective

Compose the already qualified Bitrix24 CRM adapter into the production
runtime, expose its account/OAuth/health lifecycle in the integration catalog,
and keep CRM operations separate from generic product synchronization.

## Deliverables

- [x] common host-owned HTTPS transport and Bitrix24 registry admission;
- [x] strict tenant-scoped `portal_host` runtime configuration;
- [x] OAuth access-token delivery through the existing refresh runtime;
- [x] exact CRM entity/product-row capability admission and registry ports;
- [x] generated catalog/UI support for the working CRM surface;
- [x] runtime matrix, frontend tests and architecture documentation updates.

## Explicit exclusions

Generic product sync, CRM event subscriptions, multifields, activities,
timeline/tasks, invoices/payments, custom SPA schemas and provider-specific
installation lifecycle remain separate follow-up work.

## Validation

The repository checks must pass with the pinned Go toolchain, including the
Bitrix24 connector/runtime tests, runtime-support generation, contracts,
architecture and frontend build gates. Live OAuth/portal qualification is an
environment fact and must use a dedicated non-production Bitrix24 tenant.
