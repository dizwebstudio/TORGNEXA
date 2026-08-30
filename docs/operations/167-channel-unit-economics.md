# Юнит-экономика по каналам

Task 167 добавляет фактический отчёт `unit_economics_by_channel` в разделе
«Отчёты». Это управленческий contribution margin, а не бухгалтерский GL, налоговая
декларация или расчёт cash-flow.

## Как читать отчёт

В фильтре «База» выбирается ровно одна дата признания: `order_accrual`
(заказ), `settlement` (взаиморасчёт) или `cash` (подтверждённая выплата).
Текущий PostgreSQL runtime публикует проверенную order-accrual агрегацию; для
settlement/cash без соответствующего source watermark строка должна оставаться
неопубликованной, а не смешиваться с заказами.

Формула версии `channel-unit-economics-v1`:

```text
gross merchandise value
- discounts - cancellations - refunds_and_returns
= net revenue

net revenue - commission - payment fee - fulfilment - storage
- advertising - promotion - COGS - penalties + compensation
= contribution profit
```

`payout` показывается только как cash/reconciliation показатель и никогда не
прибавляется к продажам. Все суммы — целые минимальные денежные единицы с
валютой, а маржа хранится в базисных пунктах.

## Качество и покрытие

Каждая строка содержит `quality_status` и `coverage_percent`:

- `complete` — все необходимые источники наблюдены;
- `partial` — отсутствует историческая себестоимость или часть переменных
  расходов;
- `unmatched` — заказ/расход нельзя однозначно связать с каналом и он попал в
  `unattributed`;
- `conflict` — обнаружена спорная запись settlement;
- `mixed_currency`/`unsupported` — расчёт остановлен до появления FX или
  поддержанного типа факта.

Отсутствующий факт не отображается как `0 ₽`. Исторический COGS принимается
только из `unit_economics_cost_snapshots` с `cost_as_of`; текущая карточка цены
не используется для старых заказов. Settlement dedup выполняется по
`source_system + source_account + provider_ref`, поэтому повторная выгрузка не
удваивает комиссию. Исправления создают новый calculation run с новым
`input_digest`.

## Каналы и источники

`channel_ref` — стабильная tenant-scoped ссылка на connector account/store или
явное mapping. Имя провайдера из пользовательского payload не является ключом.
Сопоставление заказов хранится в `unit_economics_order_attributions`; неизвестные
и неоднозначные назначения объясняются reason code.

Авторитетными остаются Orders/OrderItems, Payments/Refunds, SettlementEntry,
returns, рекламные/промо-факты, историческая valuation policy и Task 089b FX
ConversionSnapshot. ClickHouse используется только как перестраиваемая
аналитическая проекция. Run metadata, cost snapshots и quality evidence
хранятся в PostgreSQL с forced RLS.

## API и экспорт

```text
GET /api/v1/reports
GET /api/v1/reports/unit_economics_by_channel?basis=order_accrual&from=...&to=...&channel_ref=...
```

Доступ требует `reports.read`; scope организации и рабочего пространства берётся
из авторизации. CSV/PDF экспортирует тот же bounded snapshot и не пересчитывает
его новым курсом или формулой. Диапазон ограничен 366 днями, а результат — 200
строками.

Для production Compose необходимо применить migration `000034_channel_unit_economics.sql`,
после чего заполнить synthetic channel mappings/cost snapshots при наличии
исторической себестоимости. Сырые provider payloads, токены, банковские данные и
PII в отчёт/события/логи не попадают.
