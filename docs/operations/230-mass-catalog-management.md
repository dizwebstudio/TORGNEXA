# Task 230: массовое управление каталогом

Раздел `/catalog/bulk` — единое рабочее место для изменений карточек по нескольким
каналам. Каноническими источниками остаются Product/PIM и Offer; bulk-контур
хранит только снимок выбора, channel projection, preview и результат операции.

## Рабочий процесс

1. Получите список каналов через `GET /api/v1/catalog/bulk/summary` и проверьте
   `state` и точные `capabilities`. `qualified` не выставляется из браузера:
   для настоящего канала его выдаёт только connector control plane после
   qualification evidence.
2. Передайте в `POST /api/v1/catalog/bulk/previews` immutable selection snapshot,
   нормализованные projections и typed changes. Максимум — 1 000 SKU, 8 каналов
   и 8 000 строк. Свободный provider JSON и секреты не принимаются.
3. Проверьте rows: before/after digest, changed fields, taxonomy/mapping
   freshness, quality/compliance diagnostics и `eligible`. Заблокированная строка
   не попадает в apply вместе с валидной.
4. Для применения используйте `POST /api/v1/catalog/bulk/apply` с уникальным
   `Idempotency-Key`, `Approval-Request-ID` и подтверждением именно этого
   preview. Операция ставится в очередь по отдельным channel/account partitions.
5. Историю читайте через cursor API:
   `GET /api/v1/catalog/bulk/previews` и `GET /api/v1/catalog/bulk/runs`.
   Результаты строк не объединяются в общий success: доступны `applied`,
   `rejected`, `unknown`, `skipped` и `manual_attention`.
6. Для нормализованного read-after-write отправьте observation в
   `POST /api/v1/catalog/bulk/runs/{run_id}/reconcile`. Несовпадение digest и
   статус `unknown` создают `needs_attention`; повторная запись вслепую не
   выполняется.

## Типы изменений

Поддерживаются `set`, `replace`, `append`, `remove`, `normalize`, `copy`,
операции категорий/атрибутов/вариантов, digest-only media (`add_media`,
`replace_media`, `remove_media`, `reorder_media`), а также integer price и stock.
Для media обязательны released/safe asset, MIME, размер и размеры изображения;
удаление из channel projection не удаляет исходный PIM asset.

Цены идут в минимальных единицах валюты и проходят существующие floor/margin
guards. Bulk не меняет promotion price скрытым действием и не создаёт второй
price/inventory ledger.

## Остановка и восстановление

`GET/POST /api/v1/catalog/bulk/kill-switch` показывает или добавляет
versioned workspace emergency stop. Включение останавливает новые apply intents,
но не удаляет preview/run evidence, не переписывает PIM и не отменяет уже
принятый remote effect. После проверки mapping, taxonomy и connector state
перезапустите только разрешённые строки через новую preview/approval операцию.

Preview и run сохраняются append-only в PostgreSQL с FORCE RLS. Cursor истории
не раскрывает данные другого workspace. В JSON/evidence не сохраняются токены,
raw provider payload, private keys, необработанные upload-данные или лишний PII.

## MCP и qualification

MCP-инструмент `commerce.catalog.bulk.preview` повторяет bounded validation и
является только dry-run. Применение доступно только authenticated HTTP route с
approval и idempotency.

Синтетическая qualification проверяет core, API, migration, SDK, MCP, frontend,
лимит 1 000 SKU, partial/unknown outcomes и kill switch:

```bash
make mass-catalog-qualification
```

WB, Ozon и другие реальные кабинеты остаются `qualification_required`, пока не
сохранены versioned evidence официальной taxonomy, exact field capability,
remote write и read-after-write в sandbox/live topology. Наличие манифеста,
health-check или SDK само по себе запись не включает.
