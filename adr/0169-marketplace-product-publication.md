# ADR-0169 — публикация товаров на marketplace

Status: Accepted

## Context

WB, Ozon и Yandex Market имеют разные модели карточки, категорий,
характеристик, медиа и асинхронных операций. Нельзя передавать во внешний
коннектор внутренний `Product` и нельзя считать публикацию завершённой только
по HTTP 200. В репозитории уже существует Task 172 для отдельной операции
Yandex Market inventory write, поэтому этот Epic зарегистрирован как Task 217
при сохранении исходного номера Epic 172 в названии и документации.

## Decision

Публикация выполняется через неизменяемый provider-neutral
`marketplacepublication.Snapshot`. Snapshot содержит товар, offer/SKU,
варианты, GTIN/штрихкоды, категорию, канонические атрибуты, безопасные ссылки
на выпущенные медиа, размеры, цену, НДС и версии исходных данных. Remote ID
хранится в существующем mapping-контуре; в Core Product provider-specific поля
не добавляются.

В Connector SDK добавлен типизированный `ProductPublicationWriter` с
операциями create/update/variant/attributes/media/archive/unarchive/
publish/unpublish/status-read, dry-run, idempotency key, нормализованным
accepted/processing/published/rejected/unknown результатом и retry-safe
ошибками. Для первой runtime-квалификации допущены WB, Ozon и Yandex Market.
Остальные marketplace-коннекторы остаются fail-closed до отдельного
официального capability audit.

HTTP API сначала проверяет tenant scope, активный marketplace account,
`products.write`, актуальный Product Quality receipt и approval для реальной
записи. Worker сохраняет snapshot и operation в PostgreSQL, использует
tenant-scoped idempotency, блокировку очереди и compare-and-swap переходы.
Неоднозначный внешний исход не превращается в published: он остаётся
`unknown` или `needs_attention` до повторного чтения/reconciliation.

## Capability matrix

| Provider | Create/update | Async status | Read-after-write | Media/attributes | Admission |
| --- | --- | --- | --- | --- | --- |
| Wildberries | Content API card create/update, SKU/barcodes | Provider request ID | Bounded cards read | Explicit bridge required; unsupported operation is rejected safely | `products.write`, qualification gate |
| Ozon | Product import and import-info task status | Import task ID | Import status and bounded product read | Explicit bridge required; unsupported operation is rejected safely | `products.write`, qualification gate |
| Yandex Market | Business offer mappings update | API applies changes asynchronously | Bounded business offer read | Explicit bridge required; unsupported operation is rejected safely | `products.write`, qualification gate |
| Megamarket, Magnit Market, AliExpress RU, Lamoda, М.Видео | Not admitted by this Epic | — | Existing read/health surfaces only | Denied until provider audit | No `products.write` admission |

The matrix deliberately records denied/deferred functionality. A provider is
not marked ready because an endpoint merely exists: official request/response
fixtures, credentials scope, rate limits, async behavior and reconciliation
must pass the conformance gate.

## Consequences

The platform gets one durable publication contract and can represent an
asynchronous provider result without lying to the operator. Provider adapters
remain responsible for category and transport details, while Core stays free of
WB/Ozon/Yandex branches. A provider cannot be called through this surface until
its capability and fixture evidence is admitted.

## Alternatives considered

Keeping publication inside the existing storefront `ProductWriter` was
rejected: it would mix marketplace-specific card semantics with a smaller
storefront contract and would make async moderation indistinguishable from a
successful HTTP write. Adding remote IDs to Core Product was rejected because
one local product can have several account-specific mappings.

## Compatibility impact

Existing storefront synchronization, Product/Offer structs and connector
readers remain compatible. The new API and SDK methods are additive. Existing
marketplace accounts do not start writing until `products.write` is explicitly
enabled and a quality/approval gate passes.

## Migration and data impact

Migration 44 is expand-only and requires a verified production backup. It adds
tenant-scoped immutable snapshots, operation state, append-only transitions,
remote observations and drift records. No old table is rewritten and no
destructive rollback migration is supplied.

## Security and privacy impact

Credentials remain behind SecretProvider. Snapshot validation rejects raw URLs,
unsafe media references and control characters; provider response bodies are
not stored or returned. Forced RLS and append-only evidence preserve workspace
isolation and operational lineage.

## Operational impact

Workers claim bounded batches with row locks and use CAS transitions. Accepted
or processing operations are reconciled through the provider status reader;
unknown outcomes are never blindly retried. Operators can inspect normalized
errors/drifts and retry only unresolved operations after checking the external
cabinet.

Official references:

- [Wildberries: работа с карточками товаров](https://dev.wildberries.ru/docs/openapi/work-with-products)
- [Ozon Seller API](https://docs.ozon.ru/api/seller/)
- [Yandex Market: добавление товаров](https://yandex.ru/dev/market/partner-api/doc/ru/step-by-step/assortment-add-goods)
- [Yandex Market: изменение карточек](https://yandex.ru/dev/market/partner-api/doc/ru/reference/business-offer-mappings/updateOfferMappings)

## Security and privacy impact

Raw marketplace tokens remain in SecretProvider. Media enters the connector
only through a released `ReleasedObjectRef` and digest; arbitrary URLs,
quarantined objects and provider response bodies are not persisted in snapshot,
events, logs or API responses. The publication tables use forced RLS and
append-only evidence tables. The worker uses connector capabilities and never
branches on provider names in Core.

## Compatibility and migration impact

The change is additive. Existing storefront `ProductWriter` behavior is not
changed. Migration 44 adds publication snapshots, operations, append-only
events, observations and drifts; it is expand-only, high risk, requires a
backup and is compatible with old readers/writers. Deployment must verify the
catalog checksum before applying it.

## Operational impact

The operator sees preflight and operation history, including errors and
reconciliation state. Retry is available only for `unknown` and
`needs_attention`; repeated requests with the same tenant/account idempotency
key return the same operation. Rollback is capability disablement plus worker
drain; no destructive down migration is provided.

## Consequences

Provider-specific category profiles, media bridges and additional marketplace
adapters remain explicit follow-up qualification work. This is intentional:
the platform can safely queue and reconcile the admitted product writes without
claiming support for an unverified provider feature.
