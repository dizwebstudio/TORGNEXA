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
- admit product catalog read/write and inbound/outbound product sync only;
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

Inventory, prices, orders, offers/custom properties, browser automation and
webhook receipt are not claimed as working application routes. A real
self-hosted site with the REST module enabled and a non-production webhook is
still required for live qualification.

