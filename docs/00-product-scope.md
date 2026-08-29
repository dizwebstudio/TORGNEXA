# Product Scope

TORGNEXA is an open-source/self-hosted, API-first commerce and distribution operating platform. It unifies operational state and automation across sales channels, social distribution, ERP/accounting, regulated Russian systems, fulfillment, finance and developer/agent integrations.

## In scope

- marketplace: Wildberries, Ozon, Yandex Market, Megamarket, Magnit Market, AliExpress RU, Lamoda, М.Видео;
- classified/vertical: Avito, Auto.ru, CIAN;
- social/content: VK, Telegram, MAX, Instagram, Threads, OK, Rutube, Dzen, YouTube, with plugin expansion;
- ERP: 1C and MoySklad;
- PIM/MDM, pricing, inventory, orders/returns, procurement, WMS, fulfillment/PUDO;
- advertising/promotions, customer-service inbox, claims/disputes;
- settlement/payment reconciliation, P&L, unit economics and sourced FX conversion;
- payment rails including SBP, YooKassa, Robokassa and «Долями»;
- Chestny ZNAK, UKEP, MChD, EDO, KKT/OFD, VetIS/Mercury, EGAIS and Product Compliance;
- bidirectional sync, scheduled/on-demand reconciliation, import/export;
- REST, signed webhooks, n8n node/triggers, MCP/OpenClaw;
- bounded workflow automations with event/schedule triggers, typed conditions,
  notifications, reconciliation, dry-run and approval actions;
- legal-entity/counterparty master data, privacy/data governance, approvals, audit/lineage, secrets, upload security, enterprise IAM/SIEM, Cloud billing, SMS, security-edge, conformance, SRE and upgrade controls.

## Non-goals for Core

Core does not implement provider-specific branches, browser automation as a default integration technique, privileged AI bypasses, storage of raw card credentials/private signing keys, or a bundled/white-labeled n8n runtime.

## Distribution model

Community self-hosted is the architectural baseline. Cloud/Enterprise add managed operations, HA, enterprise secret/HSM integrations, governance, support and commercial entitlements without changing the public connector/plugin model.
