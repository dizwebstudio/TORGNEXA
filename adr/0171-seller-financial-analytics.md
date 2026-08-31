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
   `FinancialFact` и `SaleLineFact`. Факт содержит source ref, basis, валюту,
   дату, idempotency key, связь с order/SKU/channel и quality state; raw
   payload, токены и банковские реквизиты исключены.
2. В Task 174 v1 P&L считается по формуле:

   ```text
   gross sales - discounts - cancellations - refunds = net sales
   net sales - FIFO COGS - commission - payment fee - logistics - storage
   - advertising - promotion - penalties + compensation = contribution profit
   ```

   Все деньги — integer minor units с ISO-валютой. В одном snapshot выбирается
   только один basis: `order_accrual`, `settlement` или `cash`. `payout` не
   увеличивает выручку и используется в cash/reconciliation view.
3. FIFO использует exact fixed-point quantity: receipts создают layers,
   продажи/списания/quarantine потребляют их по FIFO, transfers переносят
   layers между складами, а возврат создаёт новое поступление с указанной
   стоимостью. Нет evidence — `missing_cogs`, не ноль.
4. Затраты детерминированно привязываются к `channel_ref`/`order_id`/SKU.
   Неразнесённое получает `unattributed`, оценочное и спорное — quality
   reason. Cash отдельно хранит payout, bank receipt и подтверждённые расходы;
   settlement components повторно не прибавляются.
5. Каждый расчёт — immutable run/snapshot с версиями формулы и политик,
   input digest, coverage и quality. Тот же tenant-scoped idempotency key
   возвращает исходный snapshot, поздний факт создаёт новый run.
6. API даёт P&L, cash flow, unit economics, quality, detail и manual run;
   CSV/PDF читает сохранённый snapshot. Worker ежедневно обрабатывает
   предыдущий UTC-день, не блокируя транзакции.
7. Доступ разделён на `finance.reports.read`,
   `finance.reports.detail.read` и `finance.reports.write`; все запросы
   tenant-scoped, bounded и проходят forced RLS.

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
