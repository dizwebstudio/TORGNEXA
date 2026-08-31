# ADR-0171: Автоматическая финансовая аналитика продавца

Status: Accepted

## Context

В TORGNEXA уже есть канонические заказы, платежи, возвраты, settlement
ledger, расчёт юнит-экономики из Task 167, исторические snapshots
себестоимости и аналитическая проекция ClickHouse. Нужен управленческий
финансовый отчёт, который объясняет прибыль продавца и не смешивает её с
выплатой marketplace или бухгалтерским GL.

Новый финансовый ledger для этой задачи не создаётся. Источниками остаются
существующие Order/OrderItem, SettlementEntry, payment/refund, логистика,
реклама, закупки/приёмка и FX evidence. PostgreSQL хранит операционную истину
и calculation evidence; ClickHouse может быть только перестраиваемой
проекцией.

## Decision

1. Ввести один provider-neutral calculation engine поверх нормализованных
   `FinancialFact` и `SaleLineFact`. Каждый факт содержит bounded source
   reference, basis, валюту, дату, idempotency/reference key, связь с
   order/SKU/channel и quality state. Raw provider payload, токены и банковские
   реквизиты в этот слой не попадают.
2. В Task 174 v1 управленческий P&L считается по формуле:

   ```text
   gross sales - discounts - cancellations - refunds = net sales
   net sales - FIFO COGS - commission - payment fee - logistics - storage
   - advertising - promotion - penalties + compensation = contribution profit
   ```

   Все деньги — integer minor units с ISO-валютой. P&L использует явный
   `order_accrual`, `settlement` или `cash` basis; в одном snapshot basis не
   смешивается. Выплата (`payout`) не увеличивает выручку и используется в
   cash/reconciliation view.
3. Историческая себестоимость оценивается FIFO-движком на exact fixed-point
   quantity. Поступления создают layers, продажи/списания/карантин потребляют
   их в порядке FIFO, а перемещения переносят остаточные layers между
   складами. Возврат создаёт новое поступление с явно переданной стоимостью.
   При нехватке historical stock evidence результат содержит
   `missing_cogs`/`historical_cogs_unavailable`, а не нулевую себестоимость.
4. Расходы агрегируются детерминированно по доступной связи
   `channel_ref`/`order_id`/`SKU`. Неразнесённые факты попадают в отдельное
   `unattributed` ведро. Оценочный или спорный факт сохраняет quality reason и
   не маскируется как подтверждённый расход.
5. Cash view хранит payout, bank receipt и подтверждённые платежные категории
   отдельно. Settlement-компоненты не прибавляются второй раз поверх payout;
   unmatched/disputed данные видны в quality report. Начальный/конечный
   остаток не выдумывается, если нет банковского источника.
6. Каждый расчёт — immutable run и snapshot с версиями алгоритма, формулы,
   allocation/valuation/attribution policies, input digest, coverage и
   quality status. Повтор с тем же tenant-scoped idempotency key возвращает
   исходный snapshot; поздние факты создают новый run.
7. В API добавляются seller P&L, cash flow, unit economics, financial quality,
   detail route и создание manual run. Ответы и CSV/PDF export читают уже
   сохранённый snapshot и не запускают новый расчёт.
8. Worker ежедневно материализует предыдущий UTC-день. Транзакционные
   операции не зависят от успешности расчёта; ошибка расчёта логируется как
   deferred и остаётся доступной для повторного запуска.
9. Доступ к отчётам разделён на `finance.reports.read`,
   `finance.reports.detail.read` и `finance.reports.write`. Все запросы
   tenant-scoped, ограничены периодом/строками и проходят forced RLS.

## Consequences

Оператор получает воспроизводимые P&L, cash и unit-economics views с
объяснением источников и качества. Payout остаётся отдельным cash-фактом, а
корректировка входных данных создаёт новый digest и новый snapshot. Неполные
источники видны пользователю и не превращаются в ложную прибыль.

Цена решения — необходимость дозаполнить source mappings, bank receipts, FX
и historical COGS, прежде чем строка станет `complete`. Эти данные не могут
появиться скрытой веткой по имени провайдера в Core.

## Alternatives considered

Создание второго финансового ledger отклонено: settlement, payments, orders и
WMS уже имеют собственные authoritative источники. Пересчёт отчёта напрямую
из ClickHouse отклонён, потому что проекция disposable и не должна становиться
операционной истиной. Подстановка текущей закупочной цены или нулевых COGS
для старых продаж отклонена как источник неправильной прибыли.

## Compatibility impact

OpenAPI и generated SDK расширяются аддитивно. Existing profitability what-if
endpoint и Task 167 report остаются совместимыми. Новые маршруты используют
отдельные finance permissions и возвращают только bounded snapshot values,
source references и quality metadata; provider-specific connector contracts
не изменяются.

## Migration and data impact

Миграция `000046_seller_financial_analytics.sql` только добавляет
`financial_calculation_runs`, snapshots, quality issues и status events.
Исходные ledger-таблицы не переписываются. Таблицы tenant-scoped, snapshots,
quality issues и events append-only; для high-risk миграции требуется backup.
Изменение формулы или valuation policy создаёт новую версию и новый digest.

## Security and privacy impact

Все операции используют authenticated tenant/workspace scope и forced RLS.
В snapshots и ответах сохраняются только bounded IDs, source refs, hashes,
суммы и quality reasons. Токены, raw provider payloads, private keys,
банковские реквизиты и лишние PII запрещены; detail и manual run разделены
отдельными permissions.

## Operational impact

Worker ежедневно строит предыдущий полный UTC-день по idempotency key. Сбой
расчёта не блокирует транзакционные операции и помечается deferred. Откат
выполняется отключением `finance.reports.*`, drain worker и возвратом UI к
предыдущему маршруту; destructive down migration не применяется, уже
опубликованные snapshots не редактируются.
