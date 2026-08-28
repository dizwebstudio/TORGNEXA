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
| 1С-Битрикс | product read/write | inbound + outbound | required |
| Magnit Market | product read | inbound | required |
| Megamarket | product read | inbound | required |
| Medusa | product read/write | inbound + outbound | required |
| МойСклад | ERP catalog read | inbound | no |
| 1C | ERP catalog read | inbound | required |
| OpenCart | product read/write | inbound + outbound | required |
| Ozon | product read | inbound | no |
| PrestaShop | product read | inbound | required |
| Shopify | product read/write | inbound + outbound | required |
| Shopware 6 | product read/write | inbound + outbound | required |
| Wildberries | product read | inbound | no |
| WooCommerce | product read/write | inbound + outbound | required |
| Yandex Market | product read | inbound | required |
| Magento (Adobe Commerce) | product read/write | inbound + outbound | required |
| CS-Cart | product read/write | inbound + outbound | required |
| Saleor | product read/write | inbound + outbound | required |

The host registry also contains price-writer adapters for Yandex Market,
WooCommerce, Shopify, Medusa, Shopware 6 and Magento. The current worker has
no `prices` entity bridge, so the catalog does not present price
synchronization as an executable workflow yet.

| Separate surface | Working operations | Tenant account |
|---|---|---|
| Bitrix24 CRM | lead/deal/contact/company reads and reconciled writes; lead/deal product-row reads/replacements | OAuth 2.0 + `portal_host` |

AI connectors (`Claude (Anthropic)`, `DeepSeek`, `GigaChat`, `Kimi`,
`LM Studio`, `Ollama`, `Open WebUI`, OpenAI-compatible, `Qwen`, and
`YandexGPT`) are configured and executed in Settings → AI providers. They are
marked as a separate surface and cannot create a generic connector account.
Hosted providers use the common HTTPS transport. Ollama, LM Studio and Open
WebUI use the host-mediated local transport and accept only the explicit local
endpoint allowlist; their OpenAI-compatible non-streaming completion is the
only admitted capability. Default addresses are `http://ollama:11434/v1`,
`http://host.docker.internal:1234/v1` and `http://open-webui:3000/api`.
Claude uses the host-mediated Anthropic Messages API with an `x-api-key`; the
model and optional HTTPS Base URL proxy are selected per tenant account.

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

MAX uses the same dedicated Social surface and receipt-safe worker. Its account
stores the bot token in SecretProvider and a non-zero numeric `chat_id` as
non-secret runtime configuration. Production admission is text-only with the
provider's 4000-code-point limit; Task-042 media/webhook SDK capabilities are
not claimed as connected application workflows.

Bitrix24 is available on the dedicated CRM surface. Its account uses the
host-owned OAuth 2.0/refresh flow, keeps the lower-case `portal_host` in the
versioned non-secret runtime configuration, and passes only the current access
token to the adapter. The admitted CRM capabilities are entity reads/writes
for leads, deals, contacts and companies plus lead/deal product-row
reads/replacements. It is intentionally not a generic product-sync source;
deprecated Bitrix entity APIs, multifields and event subscriptions remain
outside the v1 runtime claim.

СДЭК, 5Post, ПЭК и «Деловые Линии» доступны на отдельной поверхности
«Доставка». Для перевозчиков можно создать кабинет, сохранить credentials в
SecretProvider и запустить проверку официального API. Для СДЭК используется
JSON с OAuth client credentials, для «Деловых Линий» — appkey и PAT. Товарная
синхронизация не заявляется; создание отправлений остаётся закрытым до
квалификации актуального API и тестового кабинета.

Ozon Доставка доступна отдельной карточкой на поверхности «Доставка». Она
использует пару `client_id`/`api_key` продавца Ozon и проверяет доступ к
`/v2/warehouse/list`. Это подготовка подключения, а не готовая выдача тарифа
или создание отправления: rates, shipment, label, tracking и ПВЗ пока не
включены в runtime.

Ozon Pay доступен отдельной карточкой на поверхности «Платежи». Его проверка
использует тот же Seller API и подтверждает только доступ ключей продавца;
активация мерчанта Ozon Pay и платёжные операции требуют отдельного договора,
endpoint-квалификации и тестового аккаунта.

The remaining 14 catalog entries are planned: Auto.ru, Avito, Chestny ZNAK,
CIAN, Diadoc, EGAIS, Instagram, Odnoklassniki, Rutube, Saby EDO, Threads,
VetIS/Mercury, VK and YouTube. Their SDK
implementations and manifests remain useful for conformance and future runtime
work, but the product no longer labels them as connectable.

The logistics family now includes CDEK, 5Post, ПЭК, «Деловые Линии» and Ozon
Доставка SDK adapters. All five expose only the separately reviewed
credential-check surface; shipment writes remain fail-closed until provider
qualification.

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
