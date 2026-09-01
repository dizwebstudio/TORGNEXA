# Финансовая полнота: банки, выплаты, COGS, FX и атрибуция

## Назначение

Task 227 добавляет evidence-контур вокруг существующих финансовых источников.
Он связывает order accrual, settlement и cash views с банком/эквайрингом,
выплатами площадок, исторической себестоимостью, FX, рекламой и промо. Это
не второй ledger и не бухгалтерская отчётность.

Матрица источников находится в
`contracts/finance/financial-completeness-matrix-v1.json`. Для каждого периода
API возвращает `coverage_percent`, `status`, компоненты, `missing_codes` и
`source_refs`. Отсутствующие данные не превращаются в нулевые суммы.

## API и интерфейс

Чтение требует `finance.reports.read`:

- `GET /api/v1/financial-completeness` — summary по basis, периоду и валюте;
- `GET /api/v1/financial-completeness/sources` — redacted source evidence с
  cursor pagination;
- `GET /api/v1/financial-completeness/findings` — очередь расхождений.
- `GET /api/v1/financial-completeness/accounts` — masked bank account bindings;
- `GET /api/v1/financial-completeness/cogs-backfills` — bounded COGS backfill
  jobs and their status.

Импорт подтверждённого redacted факта требует `finance.sources.write` и
`Idempotency-Key`:

- `POST /api/v1/financial-completeness/sources` — идемпотентная append-only
  запись банковского receipt, payout, COGS, FX, advertising или promotion
  evidence.

Для bank statement и COGS backfill предусмотрены безопасные двухфазные
операции: `POST /api/v1/financial-completeness/statements:preview` →
`POST /api/v1/financial-completeness/statements` и
`POST /api/v1/financial-completeness/cogs-backfills:preview` →
`POST /api/v1/financial-completeness/cogs-backfills`. Commit всегда требует
`Idempotency-Key`.

В «Финансовой аналитике» есть вкладка «Полнота данных». Она показывает basis,
период, coverage, количество источников, открытые findings и разбивку по
продажам, refund, payout, cash, COGS, FX, рекламе и промо. Значение
`Нет источника` визуально отличается от подтверждённой суммы.

MCP предоставляет read-only tool `commerce.finance.completeness.get` с теми же
техническими кодами basis/status. Он получает tenant/workspace из проверенной
agent identity и не принимает tenant selectors, credentials, import или
adjustment commands.

## Правила источников

`SourceRecord` содержит только masked `account_ref`, provider/source reference,
amount в minor units, валюту, состояние, quality, даты и digest. Токены,
полные банковские реквизиты, raw statement и raw provider response в него не
попадают. Повтор с тем же source identity и digest безопасен; конфликт суммы,
валюты или digest попадает в attention/reconciliation.

Bank accounts и statements имеют отдельные tenant-scoped таблицы. Credentials
хранятся только через SecretProvider. Коррекция — новая evidence/adjustment и
новый financial run, а не UPDATE существующего source или snapshot.

## Как читать результат

- `complete` — все обязательные компоненты для basis имеют подтверждённое
  evidence;
- `partial` — один или несколько обязательных источников не наблюдались;
- `stale`, `unmatched`, `disputed`, `conflict` — качество требует отдельного
  действия, даже если сумма технически известна.

FX становится обязательным, когда в окне есть валюта, отличная от reporting
currency. Payout учитывается отдельно от продаж. Реклама без approved mapping
остаётся `unattributed`, а не распределяется дважды. COGS использует
исторический FIFO/as-of слой; текущая закупочная цена не является заменой.

## Backfill и release

Backfill COGS должен быть bounded по периоду, SKU и складу, сначала создавать
preview и выпускать новую immutable report version. Старые snapshots и source
facts сохраняются. Reconciliation классифицирует timing, duplicate, missing,
unmatched, stale, disputed, `missing_cogs`, `missing_fx`,
`unattributed_advertising` и `cash_mismatch`.

`make financial-completeness-qualification` подтверждает repository boundary,
контракты и synthetic safety checks. Минимум один официальный bank/acquirer,
marketplace payout, FX и advertising source требуют credentialed sandbox/live
evidence на release topology. Без такого evidence коннектор остаётся
`partial`/`qualification_required`, а не объявляется production-ready.

Для инцидента: остановить новые imports/recalculation kill switch’ом, не
удалять подтверждённую историю, сверить source digest и создать новый run после
исправления. В логах допустимы только tenant-safe IDs, статусы и timing.
