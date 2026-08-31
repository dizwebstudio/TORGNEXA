# Task 174 — Автоматическая финансовая аналитика продавца

## Статус

`repository-complete` для принятого v1-контурa: добавлены deterministic
financial engine, FIFO valuation helper, PostgreSQL calculation snapshots,
P&L/cash/unit-economics/quality API, CSV/PDF export, generated SDK, экран
«Финансовая аналитика», ежедневный worker, migration и qualification tests.

Пользовательский номер Epic — `174`. В репозитории используется task key
`219`, потому что числовые ключи старых задач уже заняты другими работами.

## Что сделано

- **174.1 / 174.6 / 174.7** — закреплены управленческий P&L, cash basis и
  unit economics. Payout не считается продажей; contribution profit и
  маржинальные показатели считаются только из нормализованных фактов.
- **174.2** — добавлен fixed-point FIFO engine с cost layers, частичным
  списанием, несколькими складами, перемещением, возвратом, списанием и
  quarantine. При отсутствии history возвращается `missing_cogs`.
- **174.3–174.5** — введены bounded financial facts, дедупликация по
  source/account/reference, атрибуция к channel/order/SKU, disputed и
  estimated quality. Исходные settlement и заказные факты не переписываются.
- **174.8** — calculation run сохраняет период, basis, версии политик,
  input digest, coverage, quality, rows/detail/cash и immutable evidence.
- **174.9** — worker ежедневно рассчитывает предыдущий UTC-день; повтор
  безопасен по tenant-scoped idempotency key. Поздние факты обрабатываются
  новым manual/daily run, а ошибка не блокирует транзакционные операции.
- **174.10** — добавлены `/api/v1/reports/seller_profit_and_loss`,
  `/seller_cash_flow`, `/seller_unit_economics`, `/seller_financial_quality`,
  detail route и `POST /financial-runs`; поддержаны фильтры и CSV/PDF из
  конкретного snapshot.
- **174.11** — добавлен ленивый frontend-раздел с вкладками P&L, ДДС,
  юнит-экономики и качества, периодом, валютой, источниками и состояниями
  неполных данных.
- **174.12 / 174.13** — quality statuses, coverage, forced RLS, отдельное
  detail permission, bounded period/limit, redacted snapshot constraints и
  запрет raw provider payload.
- **174.14** — добавлены тесты FIFO, частичного списания, перемещения,
  недоступной себестоимости, payout exclusion, dedup/collision, missing COGS,
  API/OpenAPI parity, migration catalog, generated SDK, worker и Docker
  qualification.

## Сознательные ограничения

В текущей модели нет отдельного canonical bank-receipt source, поэтому
начальный/конечный банковский остаток и bank receipts не выдумываются. Полные
live adapters для рекламной статистики, marketplace orders, refunds/returns,
FX и банковских выписок требуют официальной connector qualification и
останутся следующим расширением. Settlement-based fees, существующие
logistics facts и released cost snapshots уже могут попасть в расчёт.

Это ограничение отражается через `missing_cogs`, `missing_fx`,
`unmatched_settlement`, `unattributed_advertising`, `partial` и `disputed`, а
не через нулевые суммы.

## Зависимости

Tasks `049`, `050`, `058`, `059`, `061`, `089b`, `164` и `167`. Новый ledger не
создаётся.
