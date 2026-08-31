# Финансовая аналитика продавца: эксплуатация

## Назначение

Финансовый контур строит воспроизводимый управленческий P&L, cash-flow и
unit-economics из уже существующих фактов. PostgreSQL хранит calculation run и
snapshot; ClickHouse, если используется, остаётся перестраиваемой проекцией.

## API

Чтение требует `finance.reports.read`:

- `GET /api/v1/reports/seller_profit_and_loss` — агрегированные строки по
  каналам;
- `GET /api/v1/reports/seller_cash_flow` — cash-basis строки;
- `GET /api/v1/reports/seller_unit_economics` — юнит-экономика;
- `GET /api/v1/reports/seller_financial_quality` — качество и coverage;
- `GET /api/v1/reports/seller_profit_and_loss/details` — детализация требует
  дополнительного `finance.reports.detail.read`.

Поддерживаются `from`, `to`, `basis`, `currency`, `channel_ref`, `sku`,
`order_id`, `run_id`, `q`, `limit`, `cursor` и `format=json|csv|pdf`. Период
ограничен 366 днями, размер страницы — 200 строками.

Создание ручного расчёта требует `finance.reports.write`, JSON с `from`, `to`,
`basis` и обязательный `Idempotency-Key`:

```http
POST /api/v1/reports/financial-runs
Idempotency-Key: seller-finance-2026-08-30
Content-Type: application/json
```

Экспорт читает конкретный snapshot; экспорт или повторный запрос не изменяют
исходные заказы, settlement, payment или inventory facts.

## Автоматический расчёт

Компонент worker раз в poll interval выбирает active tenant/workspace scopes и
строит snapshot предыдущего полного UTC-дня. Ключ имеет форму
`auto:daily:YYYY-MM-DD`; рестарт worker не создаёт дубликат. Поздние settlement,
refund или correction запускаются повторным manual/backfill run с новым
idempotency key. Ошибка расчёта логируется как deferred и не откатывает
транзакцию продавца.

## Как читать quality

- `complete` — все требуемые для строки факты доступны;
- `partial` — расчёт опубликован, но часть evidence отсутствует;
- `missing_cogs` — нет исторической FIFO/cost snapshot;
- `missing_fx` — невозможно безопасно привести валюты;
- `unmatched_settlement` — settlement не связан с order/channel;
- `unattributed_advertising` — расход рекламы не имеет однозначной
  атрибуции;
- `stale` — источник или snapshot устарел;
- `disputed` — есть спорный финансовый факт;
- `mixed_currency` — в строке нельзя безопасно суммировать валюты.

Отсутствующая сумма не трактуется как подтверждённый ноль. Сначала нужно
исправить source mapping или загрузить подтверждённый факт, затем запустить
новый calculation run.

## Миграция и откат

`000046_seller_financial_analytics.sql` — expand-only, high-risk. Перед
применением нужен проверенный backup PostgreSQL. Down migration не используется:
при проблеме отключаются `finance.reports.*`, worker переводится в drain, а
старые snapshots остаются immutable evidence.

## Troubleshooting

1. Если API отвечает `404 Financial calculation is not available`, проверьте,
   что для tenant/workspace завершён daily или manual run за нужный период.
2. Если статус `partial`, откройте quality report и исправьте конкретный
   reason/source ref; повторный запрос без нового run ничего не меняет.
3. Если payout отличается от P&L, это ожидаемо: payout относится к cash,
   а продажи и комиссии — к выбранному P&L/settlement basis. Settlement
   components не следует добавлять к payout вручную.
4. Если COGS unavailable, проверьте исторический cost snapshot и WMS movement
   evidence. Текущую закупочную цену подставлять нельзя.

Логи содержат только scope IDs, bounded status/error codes и timing. Токены,
raw provider payloads, DataMatrix/банковские реквизиты и секреты в отчёты и
логи не попадают.
