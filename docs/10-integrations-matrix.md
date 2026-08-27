# Integration Matrix

This document distinguishes three different facts which must not be conflated:

1. a connector manifest describes the SDK contract implemented by a provider package;
2. built-in runtime support means the production binary has a host transport and a canonical domain bridge for an exact capability;
3. a separate product surface may execute a connector without exposing it through Settings → Integrations.

The machine-readable source of truth for points 2 and 3 is
`contracts/connectors/builtin-runtime-support-v1.json`. The frontend projection
and Go runtime admission table are generated from this contract together with
the connector manifests. A manifest-only connector is visible for discovery,
but account creation, capability enablement and synchronization fail closed.

## Current production runtime

The generic integration runtime currently supports only the canonical
`products` entity. It does not advertise order, inventory, return, publication,
message, payment or document synchronization merely because a provider SDK
declares those capabilities.

| Connector | Working operations in Settings → Integrations | Product sync | Non-secret runtime config |
|---|---|---|---|
| AliExpress RU | product read | inbound | no |
| Magnit Market | product read | inbound | required |
| Megamarket | product read | inbound | required |
| МойСклад | ERP catalog read | inbound | no |
| 1C | ERP catalog read | inbound | required |
| OpenCart | product read/write | inbound + outbound | required |
| Ozon | product read | inbound | no |
| PrestaShop | product read | inbound | required |
| Wildberries | product read | inbound | no |
| WooCommerce | product read/write | inbound + outbound | required |
| Yandex Market | product read | inbound | required |

The host registry also contains price-writer adapters for Yandex Market and
WooCommerce. The current worker has no `prices` entity bridge, so the catalog
does not present price synchronization as an executable workflow yet.

AI connectors (`DeepSeek`, `GigaChat`, `Kimi`, OpenAI-compatible, `Qwen`, and
`YandexGPT`) are configured and executed in Settings → AI providers. They are
marked as a separate surface and cannot create a generic connector account.

CBR FX is executed by the worker as a separate Finance surface. It downloads
the explicitly dated official Bank of Russia daily document, persists immutable
foreign-currency/RUB facts and exposes them through Finance → FX rates. It does
not create a tenant cabinet because the source is global public reference data
without credentials.

Telegram is executed on the dedicated Social surface. The normal connector
account control plane stores the bot token in SecretProvider and the negative
`chat_id` as non-secret runtime configuration. Social Core API and worker
currently execute only `social.post.text`; remote receipts make crash recovery
safe without duplicate-prone automatic resend.

The remaining 19 catalog entries are planned: Auto.ru, Avito, Bitrix24, CDEK,
Chestny ZNAK, CIAN, Diadoc, EGAIS, Instagram, MAX, Odnoklassniki, Rutube, Saby
EDO, SBP, Threads, VetIS/Mercury, VK, YooKassa and YouTube. Their SDK
implementations and manifests remain useful for conformance and future runtime
work, but the product no longer labels them as connectable.

| Family | Targets | Initial focus |
|---|---|---|
| Marketplace | Wildberries, Ozon, Yandex Market, Megamarket, Magnit Market, AliExpress RU | repository-qualified baselines; manifest-declared coverage is summarized in the [marketplace connector guide](connectors/marketplaces.md) |
| Classified | Avito | listings, leads/messages, stats where officially permitted |
| Vertical | Auto.ru, CIAN | partner/feed/API audit; Vehicle/Property mapping |
| Social | VK, Telegram, MAX, Instagram, Threads, OK, Rutube, Dzen, YouTube | content/media/comments/analytics by capability |
| ERP | 1C, MoySklad | catalog, stock, orders, shipments, returns, finance mapping |
| Government | Chestny ZNAK, VetIS/Mercury, EGAIS | status/docs/reconciliation; regulated writes phased and approved |
| EDO | Diadoc, Saby | provider SDK, document/status/signing workflow |
| Payments | SBP + reference card/acquirer + plugin acquirers | payment/status/webhook/refund/commission/reconciliation |
| Logistics | reference carrier + carrier plugins/PUDO | rate/create/cancel/track/label/return/capacity/issue |
| Notifications | Email/TG/MAX/Webhook/n8n/SMS providers | transactional delivery/status/fallback with policy |
| Enterprise IAM/SIEM | LDAP/AD, SAML, SCIM, JIT; syslog/webhook/Kafka/OTLP | federation/provisioning and security-event export |
| Developer | n8n, MCP/OpenClaw, REST/webhooks | external automation and agents with scopes |

Each connector starts with a current Connector Spec using `templates/connector-spec.md`, passes `contracts/conformance/connector-conformance.yaml`, and declares unsupported capabilities explicitly. Admission to the production catalog additionally requires an explicit runtime-support record, a host-mediated transport and a canonical entity bridge. Browser automation/scraping is never the default integration method.
