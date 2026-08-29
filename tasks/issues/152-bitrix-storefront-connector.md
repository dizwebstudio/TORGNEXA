# Task 152 — 1С-Битрикс storefront connector

Status: Repository implementation complete

## Problem

The Integrations catalog had a separate Bitrix24 CRM card, but no card or
runtime adapter for self-hosted 1С-Битрикс internet shops.

## Scope

- Add a `storefront` connector with the official REST catalog surface and
  host-mediated transport;
- keep webhook credentials encrypted (`user_id` and `webhook_code`), with
  `store_host`, `base_path`, `catalog_iblock_id` and `store_currency` as
  non-secret runtime configuration;
- admit product catalog read/write, outbound regular-price sync and outbound
  integer inventory sync through warehouse documents;
- implement SDK-level active warehouse and integer stock reads;
- implement SDK-level order reads and status writes with sale REST
  reconciliation;
- show a branded 1С-Битрикс card in Settings → Integrations → Интернет-магазины;
- document the REST-module/webhook prerequisite and the explicit runtime gaps.

## Acceptance criteria

- `1С-Битрикс` is distinct from the existing `Bitrix24` CRM connector;
- product reads use bounded cursor pagination and validate the configured
  information-block ID;
- writes use exact `xmlId` reconciliation and read-after-write verification;
- malformed responses, remote errors and ambiguous outcomes fail closed;
- generated catalog, runtime registry, architecture policy/review, docs and
  conformance evidence are synchronized;
- Go, frontend and contract checks pass.

## Explicit exclusions

Inbound prices, offers/custom properties, browser automation and webhook
receipt are not claimed as working application routes. Order runtime routing
requires an explicit `order_statuses` map for all canonical lifecycle values.
Inventory writes require an explicit `warehouse` entity mapping to the configured Bitrix warehouse. A real
self-hosted site with the REST module enabled and a non-production webhook is
still required for live qualification.
