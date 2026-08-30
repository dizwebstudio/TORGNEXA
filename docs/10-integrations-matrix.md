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

The generic integration runtime supports the canonical `products` entity in
both directions where the generated runtime-support contract admits it. The
configured connector registry also resolves qualified price, inventory and
order read adapters and the reconciliation worker now provides the inbound
price/inventory source and canonical local upsert bridge. The `commerce-sync`
worker consumes `commerce.catalog.product_changed.v1`, loads
the canonical product, resolves the tenant-scoped `product` mapping (or
creates it after a confirmed product upsert), and records an idempotent local
receipt. It also explicitly routes inbound reconciliation and outbound `prices` and `inventory` events
for the admitted storefront writers. Inbound price updates use the mapped
offer and exact currency minor units; inbound inventory updates use mapped
offer/warehouse pairs and update the canonical position through the inventory
ledger path. It does not advertise return, publication, message, payment or
document synchronization merely because a provider SDK declares those
capabilities.

| Connector | Working operations in Settings → Integrations | Product sync | Non-secret runtime config |
|---|---|---|---|
| AliExpress RU | product read | inbound | no |
| 1С-Битрикс | product/price/inventory reads; product/price/inventory writes via warehouse documents; order read/status write | products, prices, inventory, orders inbound + outbound | required (`catalog_iblock_id`, `price_type_id`, `order_statuses`) |
| Magnit Market | product/price/inventory/order reads | products, prices, inventory, orders inbound | required |
| Megamarket | product/inventory/order reads | products, inventory, orders inbound | required |
| Medusa | product/price/inventory/order reads; product/price/inventory writes; order cancellation | products, prices, inventory, orders inbound + outbound | required |
| МойСклад | ERP catalog read | inbound | no |
| 1C | ERP catalog read | inbound | required |
| OpenCart | product/price/inventory reads; product/price/inventory writes через shop-local bridge `extension/torgnexa/api/*` | products, prices, inventory inbound + outbound | required; установить `torgnexa.ocmod.zip` |
| Ozon | product/inventory read | products, inventory inbound | no |
| PrestaShop | product/price/inventory reads; price/inventory writes ([Docker Webservice smoke](connectors/prestashop/docker-smoke.md)) | products inbound; prices and inventory inbound + outbound | required |
| Shopify | product/price/inventory/order reads; product/price/inventory writes; order cancellation | products, prices, inventory, orders inbound + outbound | required |
| Shopware 6 | product/price/inventory/order reads; product/price/inventory writes; order cancellation | products, prices, inventory, orders inbound + outbound | required |
| Wildberries | product/inventory read | products, inventory inbound | no |
| WooCommerce | product/price/inventory/order reads; product/price/inventory writes; order status write ([Docker smoke](connectors/woocommerce/docker-smoke.md)) | products, prices, inventory, orders inbound + outbound | required |
| Yandex Market | product/price/inventory/order reads; price write | products, prices inbound; prices outbound; inventory and orders inbound | required |
| Magento (Adobe Commerce) | product/price/inventory/order reads; product/price/inventory writes; order cancellation | products, prices, inventory, orders inbound + outbound | required |
| CS-Cart | product read/write | inbound + outbound | required |
| Saleor | product/price/inventory/order reads; product/price/inventory writes; order cancellation | products, prices, inventory, orders inbound + outbound | required |

The host registry contains SDK price, inventory and order readers for the qualified
marketplace/storefront adapters above. 1С-Битрикс, Magento, Medusa, OpenCart,
PrestaShop, Saleor, Shopify, Shopware, WooCommerce and Yandex Market are
admitted to the production price worker route. The outbound inventory route is
admitted for 1С-Битрикс, Magento, Medusa, OpenCart, PrestaShop, Saleor, Shopify,
Shopware and WooCommerce; every event requires a matching
tenant-scoped warehouse mapping. Inventory events
require an explicit tenant-scoped warehouse mapping. A product, price or inventory domain event is consumed by the
dedicated `torgnexa.commerce-sync.v1` group, resolved through the tenant's
enabled outbound policy and the corresponding `product` or `offer` mapping,
sent with a deterministic idempotency key, and recorded in
`sync_local_receipts` after an applied or duplicate receipt. Transient
provider failures go through the normal Kafka retry topic; malformed events,
missing offer mappings, product mapping collisions and non-retryable provider
responses go to the DLQ.

Payment accounts admitted to the finance surface also have a bounded background
reconciliation sweep. It runs at most every five minutes for the last 48 hours,
matches returned settlement observations by account, remote ID and exact money,
including provider refund observations when the audited rail exposes them,
and applies only valid canonical status transitions. An unknown status, amount
mismatch or unavailable gateway remains visible as stale/manual attention and
is never converted into a successful local payment.

| Separate surface | Working operations | Tenant account |
|---|---|---|
| Bitrix24 CRM | lead/deal/contact/company reads and reconciled writes; lead/deal product-row reads/replacements | OAuth 2.0 + `portal_host` |

AI connectors (`Claude (Anthropic)`, `DeepSeek`, `GigaChat`, `Google Gemini`,
`Grok (xAI)`, `Kimi`, `LM Studio`, `Ollama`, `Open WebUI`, OpenAI-compatible,
`Qwen`, and `YandexGPT`) are configured and executed in Settings → AI
providers. They are
marked as a separate surface and cannot create a generic connector account.
Hosted providers use the common HTTPS transport. Ollama, LM Studio and Open
WebUI use the host-mediated local transport and accept only the explicit local
endpoint allowlist; their OpenAI-compatible non-streaming completion is the
only admitted capability. Default addresses are `http://ollama:11434/v1`,
`http://host.docker.internal:1234/v1` and `http://open-webui:3000/api`.
Claude uses the host-mediated Anthropic Messages API with an `x-api-key`; the
model and optional HTTPS Base URL proxy are selected per tenant account.
Google Gemini uses the official `generateContent` endpoint and the
`x-goog-api-key` header; Grok uses xAI Chat Completions with a Bearer key. Both
providers currently expose only bounded non-streaming text completion.

Midjourney is not offered as a connectable AI account: its official policy
states that it does not provide a public API and prohibits third-party
automation. It therefore cannot be represented as a working TORGNEXA runtime
connector without violating the provider's terms.

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

СДЭК, 5Post, ПЭК, «Деловые Линии» и «Почта России» доступны на отдельной поверхности
«Доставка». Для перевозчиков можно создать кабинет, сохранить credentials в
SecretProvider и запустить проверку официального API. Для СДЭК используется
JSON с OAuth client credentials, для «Деловых Линий» — appkey и PAT, для
«Почты России» — токен приложения и ключ пользователя. Товарная синхронизация
не заявляется; для CDEK, ПЭК, «Деловых Линий» и «Почты России» доступно только
bounded read-only чтение справочника ПВЗ/терминалов через `pickup.points.read`.
Создание
отправлений, расчёт, статусы и этикетки остаются закрытыми до квалификации
актуального API и тестового кабинета.

Ozon Доставка доступна отдельной карточкой на поверхности «Доставка». Она
использует пару `client_id`/`api_key` продавца Ozon и проверяет доступ к
`/v2/warehouse/list`. Это подготовка подключения, а не готовая выдача тарифа
или создание отправления: rates, shipment, label, tracking и ПВЗ пока не
включены в runtime.

Ozon Pay доступен отдельной карточкой на поверхности «Платежи». Его проверка
использует тот же Seller API и подтверждает только доступ ключей продавца;
активация мерчанта Ozon Pay и платёжные операции требуют отдельного договора,
endpoint-квалификации и тестового аккаунта.

The former 14 planned entries are now grouped into explicit category surfaces.
Each card supports tenant-scoped credential enrollment and an authenticated
health check; none is advertised as a generic product-sync or publication
route until its domain bridge is qualified.

| Category surface | Providers | Current runtime contract |
|---|---|---|
| Объявления и вертикали | Auto.ru, Avito, CIAN | credential + official API health check |
| Социальные сети | Instagram, Odnoklassniki, Rutube, Threads, VK, YouTube | credential + official API health check |
| ЭДО | Diadoc, Saby EDO | credential + operator-supplied HTTPS health endpoint |
| Госсистемы | Chestny ZNAK, EGAIS, VetIS/Mercury | credential + operator-supplied HTTPS health endpoint |

This is a deliberate health-only admission: a healthy account proves that the
stored credential can reach the configured endpoint, not that an unqualified
regulated write, publication or document workflow is available. The catalog
therefore has zero `planned` entries while retaining fail-closed domain
capabilities.

The logistics family now includes CDEK, 5Post, ПЭК, «Деловые Линии», «Почта России»
and Ozon Доставка SDK adapters. CDEK, ПЭК, «Деловые Линии» and «Почта России»
expose a bounded, read-only `pickup.points.read` application route for their
official ПВЗ/terminal directories; shipment writes, rates and other carrier
operations remain fail-closed until provider qualification. 5Post and Ozon
Доставка expose only the separately reviewed credential-check surface.

Lamoda и М.Видео также доступны в категории «Маркетплейсы» как health-only
карточки. Для них можно завести tenant-scoped кабинет и выполнить bounded
проверку настроенного HTTPS endpoint, но товары, цены, остатки и заказы не
передаются в worker до квалификации актуального партнёрского API.

«Долями» доступен в категории «Платежи» как health-only карточка. Официальный
API использует логин/пароль и mTLS-сертификат; TORGNEXA проверяет только
настроенный endpoint, а Create/Commit/Cancel/Info/Refund и вебхуки требуют
отдельной квалификации.

| Family | Targets | Initial focus |
|---|---|---|
| Marketplace | Wildberries, Ozon, Yandex Market, Megamarket, Magnit Market, AliExpress RU, Lamoda, М.Видео | repository-qualified baselines plus health-only Lamoda/М.Видео cards; manifest-declared coverage is summarized in the [marketplace connector guide](connectors/marketplaces.md) |
| Classified | Avito | listings, leads/messages, stats where officially permitted |
| Vertical | Auto.ru, CIAN | partner/feed/API audit; Vehicle/Property mapping |
| Social | VK, Telegram, MAX, Instagram, Threads, OK, Rutube, Dzen, YouTube | content/media/comments/analytics by capability |
| ERP | 1C, MoySklad | catalog, stock, orders, shipments, returns, finance mapping |
| Government | Chestny ZNAK, VetIS/Mercury, EGAIS | status/docs/reconciliation; regulated writes phased and approved |
| EDO | Diadoc, Saby | provider SDK, document/status/signing workflow |
| Payments | SBP, YooKassa, Robokassa, «Долями» + reference card/acquirer + plugin acquirers | payment/status/webhook/refund/commission/reconciliation; «Долями» пока health-only |
| Logistics | reference carrier + carrier plugins/PUDO | rate/create/cancel/track/label/return/capacity/issue |
| Notifications | Email/TG/MAX/Webhook/n8n/SMS providers | transactional delivery/status/fallback with policy |
| Enterprise IAM/SIEM | LDAP/AD, SAML, SCIM, JIT; syslog/webhook/Kafka/OTLP | federation/provisioning and security-event export |
| Developer | n8n, MCP/OpenClaw, REST/webhooks | external automation and agents with scopes |

Each connector starts with a current Connector Spec using `templates/connector-spec.md`, passes `contracts/conformance/connector-conformance.yaml`, and declares unsupported capabilities explicitly. Admission to the production catalog additionally requires an explicit runtime-support record, a host-mediated transport and a canonical entity bridge. Browser automation/scraping is never the default integration method.
