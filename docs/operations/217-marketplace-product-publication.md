# Публикация товаров на marketplace

Этот документ описывает рабочий контур публикации карточек из TORGNEXA в
WB, Ozon и Yandex Market.

## Как проходит публикация

1. Оператор или интеграция формирует snapshot конкретного товара, offer и
   marketplace account.
2. Preflight проверяет quality receipt, версии каталога/PIM/цены/медиа/mapping,
   категорию, SKU/GTIN, валюту и размерные поля.
3. API требует `products.write`, активный marketplace account и approval для
   реальной записи. Dry-run не вызывает внешний API.
4. Snapshot и operation сохраняются в PostgreSQL. Повтор того же
   `Idempotency-Key` возвращает ту же операцию.
5. Worker вызывает только конкретный connector. Возможные состояния:
   `queued → sending → accepted/processing → published`, либо
   `rejected`, `unknown`, `needs_attention` или `cancelled`.
6. Async provider result читается отдельно. HTTP 200 сам по себе не является
   доказательством публикации.

## Что передаётся в connector

Connector получает `ProductPublicationRequest` с immutable snapshot. Внутри
snapshot нет provider token, raw provider JSON, произвольного URL или ключа
объекта. Для медиа разрешён только `ReleasedObjectRef` выпущенного pipeline
объекта и его digest. Provider-specific remote IDs возвращаются в нормальном
receipt и могут быть записаны в существующий EntityMapping-контур.

## Runtime matrix

| Канал | Включённые операции | Async/read-after-write | Что пока закрыто |
| --- | --- | --- | --- |
| WB | create/update card, variant/SKU payload, status read | request ID + bounded cards read | media bridge и provider-specific attributes до отдельной qualification |
| Ozon | product import, update import, task status | import task ID + bounded status | media и field-level attribute bridge |
| Yandex Market | business offer mappings update, status read | применение может быть отложено | media и provider-specific attributes |
| Остальные marketplace | read/health существующих контуров | — | `products.write` denied |

Ошибки unsupported, конфликт SKU/mapping и неоднозначный timeout не считаются
успешной публикацией. Для `unknown` и `needs_attention` оператор может
запросить retry после проверки внешнего кабинета.

## Миграция и эксплуатация

Migration 44 является expand-only и требует backup перед production rollout.
Перед применением deploy проверяет SHA-256 в `migrations/catalog.json` и
`deploy/postgres/catalog.tsv`. Evidence операции хранится append-only; raw
ответы провайдеров туда не попадают. Для отказа от функции достаточно
отключить capability и остановить admission worker — destructive down migration
не используется.

## Официальные API

- [Wildberries OpenAPI — работа с товарами](https://dev.wildberries.ru/docs/openapi/work-with-products)
- [Ozon Seller API](https://docs.ozon.ru/api/seller/)
- [Yandex Market — добавление товаров](https://yandex.ru/dev/market/partner-api/doc/ru/step-by-step/assortment-add-goods)
- [Yandex Market — изменение карточки](https://yandex.ru/dev/market/partner-api/doc/ru/reference/business-offer-mappings/updateOfferMappings)
