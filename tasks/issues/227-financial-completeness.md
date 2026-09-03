# Task 227 — Финансовая полнота: банки, выплаты, COGS, FX и атрибуция рекламы

## Статус

`repository-complete` — добавлены provider-neutral source evidence, masked bank
account/statement boundary, completeness matrix/evaluation, API/SDK, RLS и
frontend financial completeness center. Существующие Order, Payment, Settlement,
FX, COGS/FIFO и advertising projections переиспользуются; второй ledger не
создан. Общий fail-closed gate `make financial-warehouse-qualification`
проверяет retained credentialed evidence финансового и складского контуров;
сама credentialed qualification официальных accounts остаётся внешним входом
и не подменяется health-check.

## Цель

Довести финансовые отчёты от частично заполненных управленческих показателей
до воспроизводимого контура:

```text
банк/эквайринг → payout/settlement → Order/Payment/Refund
→ историческая себестоимость → FX conversion snapshot
→ реклама/промо attribution → P&L + ДДС + unit economics → reconciliation
```

«Полная» строка отчёта означает не наличие цифр во всех колонках, а наличие
подтверждённого источника, корректной даты и валюты, правила распределения и
coverage evidence. Если факт не найден, отчёт обязан показать
`missing`/`unmatched`/`unattributed`/`stale`, а не подставить ноль.

## Результат выполнения

Все подзадачи 227.1–227.14 закрыты на уровне repository boundary: source
matrix, append-only evidence, bank account/statement preview и commit,
идемпотентный source import, completeness evaluation, findings queue, API,
generated SDK, frontend, RLS/audit controls, migration catalog, documentation
и synthetic qualification gate подключены. Существующие financial runs не
переписываются и не получают скрытых нулевых значений; новый слой служит
evidence/quality projection вокруг канонических ledgers.

| Подзадача | Статус | Evidence |
|---|---|---|
| 227.1 | `closed` | ADR-0178, source matrix JSON и typed `Matrix()` |
| 227.2 | `closed` | migration 000054, `BankAccount`/`BankStatement`, masked balances, RLS и preview/commit API |
| 227.3 | `closed` | lifecycle/status boundary, cursor/SecretProvider reference и release gate для первого live connector |
| 227.4 | `closed` | payout/source kinds, stable provider refs, idempotent append и separate payout quality |
| 227.5 | `closed` | Payment/refund/payout source taxonomy, dedup/conflict handling и findings projection |
| 227.6 | `closed` | existing FIFO/as-of cost snapshots reused; missing COGS remains explicit in evaluation |
| 227.7 | `closed` | bounded backfill job model/API/UI, preview→queue flow and immutable new-run policy |
| 227.8 | `closed` | existing immutable FX 089/131 facts reused; foreign evidence requires FX coverage |
| 227.9 | `closed` | advertising/promotion source kinds, attribution status and conservation warning boundary |
| 227.10 | `closed` | deterministic completeness evaluator with basis, coverage, quality and no zero-fill |
| 227.11 | `closed` | typed findings storage/read queue with tenant RLS and immutable source evidence |
| 227.12 | `closed` | `/financial-completeness` API, Go/Python/TypeScript SDK, MCP и Financial Analytics UI |
| 227.13 | `closed` | forced RLS, append-only triggers, masked fields, SecretProvider pointer and sensitive audit |
| 227.14 | `closed` | synthetic core/API/static gates, OpenAPI parity, frontend wiring and release-gate documentation |

`repository-complete` не означает, что у репозитория появились реальные
банковские или marketplace credentials. Для production claim дополнительно
нужны retained credentialed sandbox/live evidence с connector version, scopes,
датой и redacted result.

## Что уже есть и что закрывает этот task

- Task 219 даёт seller P&L, cash-flow, unit economics, quality API, export и
  daily calculation run.
- Tasks 058–059 дают append-only settlement entries и классы reconciliation
  differences.
- Tasks 089 и 131 дают immutable historical FX facts, CBR source и exact
  conversion snapshots.
- Task 167 даёт channel attribution, cost components и финансовые quality
  statuses.
- Task 220 даёт read-only campaign/spend/performance facts.

Task 227 не создаёт второй Order, Payment, Settlement или financial ledger.
Он добавляет отсутствующие authoritative inputs, соединяет их с расчётным run
и закрывает release qualification для bank/payout/COGS/FX/advertising facts.

## Критерии финансовой полноты

Для каждого отчётного периода и выбранного basis (`order_accrual`,
`settlement`, `cash`) должны быть доступны:

1. продажи, отмены, возвраты, refunds и fees с однозначным source reference;
2. payout/settlement facts и, для cash view, подтверждённые bank/acquirer
   receipts;
3. historical COGS as-of даты движения/продажи с valuation policy;
4. FX conversion snapshot для каждой cross-currency суммы;
5. advertising/promotion spend с attribution policy, окном и coverage;
6. объяснимое распределение общих затрат и reconciliation findings.

Отсутствующий компонент снижает completeness/coverage и блокирует только те
решения, которым он необходим; весь отчёт не должен исчезать или становиться
ложно «полным».

## Подзадачи

### 227.1 — ADR финансовой полноты и матрица источников

**Зависимости:** 167, 219.

- Утвердить определения financial completeness, coverage, accrual,
  settlement и cash basis, а также правила дат для sale, refund, payout,
  bank receipt, COGS, FX и advertising spend.
- Составить матрицу `metric → canonical source → fallback → quality status →
  retention`, включая revenue, payout, fees, cash, COGS, FX, ads и promotion.
- Определить source precedence и deduplication между Order, Payment,
  SettlementEntry, payout и bank receipt; payout не является продажей.
- Зафиксировать правила распределения общих расходов, межканальных банковских
  комиссий, рекламы без SKU и валютной конвертации.
- Подготовить worked examples для полного заказа, частичного refund,
  payout с несколькими заказами и периода с missing facts.

**Acceptance:** finance/product review подтверждает формулы, знаки, даты,
coverage и source precedence; ни один missing факт не маскируется нулём или
оценкой без явного `estimated` статуса.

### 227.2 — Модель bank account, statement и receipt

**Зависимости:** 227.1, 058–059.

- Добавить tenant-scoped typed source model для bank account, statement,
  transaction, payout receipt, opening/closing balance и reconciliation link.
- Хранить только минимальные masked identifiers, currency, amount, direction,
  occurred/posted date, source reference, status и digest; полные реквизиты и
  credentials остаются в SecretProvider/secure boundary.
- Поддержать импорт statement через одобренный API или безопасный released
  file path с preview, validation, duplicate detection и commit.
- Нормализовать pending/posted/reversed/fee/transfer/unknown без потери
  original currency и банковской даты.
- Не превращать bank source в второй денежный ledger: исходные записи
  append-only, correction — отдельная adjustment/reconciliation evidence.

**Acceptance:** схема и API tenant-scoped, append-only и idempotent; duplicate
statement/transaction не удваивает cash; cross-tenant и raw bank data tests
проходят.

### 227.3 — Первый live bank connector и account lifecycle

**Зависимости:** 227.2, 226.

- Выбрать минимум один официальный bank/open-banking connector или безопасный
  statement provider для первой qualification wave.
- Реализовать account discovery, scoped auth, statement sync, cursor/watermark,
  rate limits, timeout, reauthorization, disable и reconciliation.
- Поддержать sandbox/test fixtures и live non-production smoke; bank API failure
  не должен блокировать unrelated P&L/report runs.
- Проверить matching bank receipt ↔ payout/payment/settlement по reference,
  amount, currency, date window и approved tolerance.
- Выдать `cash_ready` только при актуальном evidence; health-check банка не
  является доказательством загрузки выписки.

**Acceptance:** повтор sync восстанавливается после crash, не создаёт дублей,
  rejected/unknown transfer попадает в attention queue, а live/sandbox evidence
  содержит connector version, scope, дату и redacted result.

### 227.4 — Полный поток marketplace/acquirer payouts

**Зависимости:** 058–059, 227.1–227.3.

- Добавить typed payout/settlement batch import: sales, commissions,
  logistics, storage, penalties, compensation, refunds, reserves, payout and
  adjustments с provider references.
- Связать payout batch с orders/returns/refunds и bank receipt; один payout,
  settlement fee или bank transfer не должен учитываться дважды.
- Поддержать payout schedule, partial payout, split settlement, withholding,
  late adjustment, disputed and reversed payout.
- Сохранять original currency, source dates, remote batch IDs и matching
  confidence; неизвестная связь остаётся `unmatched`.
- Ввести reconciliation между expected, observed, settled и cash views.

**Acceptance:** fixtures с payout на несколько заказов, payout до settlement,
  частичным refund, duplicate fee, bank delay, reversal и disputed entry
  классифицируются без завышения revenue/cash/profit.

### 227.5 — Payment/acquirer cash и refund reconciliation

**Зависимости:** 227.2–227.4, Task 164.

- Соединить capture/paid/fee/refund из Payment с acquirer settlement и bank
  receipts через stable references, не переписывая Payment/Settlement facts.
- Разделить gross payment, processing fee, payout, refund, reserve и net cash;
  refund не должен появиться дважды через Payment и settlement.
- Обработать delayed capture, partial capture/refund, failed/unknown webhook,
  chargeback/dispute reference и bank reversal как отдельные outcomes.
- Сохранить approved mapping и source precedence для gateway vs bank facts.
- Выдавать manual attention для cash mismatch, который нельзя разрешить
  точным reference/amount/date rule.

**Acceptance:** один платёж и один refund дают ровно один cash effect;
  timeout после remote success не приводит к повторной записи; reconciliation
  показывает обе стороны и разницу по типу.

### 227.6 — Historical COGS и valuation as-of

**Зависимости:** 054, 165, 167, 219.

- Подключить historical cost facts из receiving, supplier offer/PO, WMS ledger,
  freight/landed cost, transfers, write-off, quarantine, return и adjustment.
- Для каждого OrderItem выбирать cost layer as-of нужной даты и warehouse с
  явной FIFO/valuation policy; текущий `Price(kind=cost)` нельзя использовать
  как замену прошлой себестоимости.
- Поддержать multi-warehouse transfer, partial receipt, return-to-stock,
  damaged/scrap и fractional quantity без отрицательных слоёв.
- Сохранять immutable valuation snapshot, source refs, algorithm version,
  allocation digest и `missing_cogs` reason при разрыве истории.
- Не разрешать отчёту или AI «восстановить» отсутствующую себестоимость без
  пометки `estimated` и утверждённой policy.

**Acceptance:** as-of tests для нескольких поставок/складов, partial receipt,
  transfer, return, write-off и late adjustment дают воспроизводимый COGS;
  история не меняется задним числом, а gap виден в coverage.

### 227.7 — Backfill исторической себестоимости и quality remediation

**Зависимости:** 227.6.

- Создать bounded backfill по периоду, SKU, складу и каналу с preview до commit;
  определить минимальную дату, coverage и список пропусков.
- Импортировать синтетические и approved historical receipts/PO/WMS facts,
  не копируя production PII.
- Поддержать ручное подтверждение source mapping и отдельные adjustment
  records; запрещено молча менять уже опубликованный financial snapshot.
- Пересчитывать только новые версии report run с сохранением старого digest,
  причины изменения и affected rows.
- Показать оператору, какие товары/дни остаются с `missing_cogs`, и дать
  безопасный retry после исправления источника.

**Acceptance:** backfill идемпотентен, bounded и tenant-scoped; повтор не
  удваивает cost layers, а delta старого/нового расчёта объяснима и аудируема.

### 227.8 — FX integration в financial runs

**Зависимости:** 089, 131, 167, 219.

- Использовать существующие immutable FX RateFact/ConversionSnapshot для
  settlement, payout, bank, COGS и advertising facts в reporting currency.
- Выбирать rate строго по `as_of`, source, pair и policy; сохранять conversion
  reference, original/result amount, rate source, rounding и snapshot digest.
- Обработать source outage, stale rate, unsupported pair, mixed currency и
  correction без implicit inversion/triangulation.
- Разделить FX gain/loss, conversion residual и исходную business margin;
  не смешивать их без утверждённой accounting policy.
- Включать FX coverage в quality report и блокировать только расчёты, которым
  требуется отсутствующий курс.

**Acceptance:** historical rerun с тем же snapshot совпадает побитно по
  результату; missing/stale/unsupported FX не превращается в RUB-ноль;
  bank/settlement/reporting conversion references совпадают.

### 227.9 — Рекламная и promotion attribution

**Зависимости:** 167.8, 220, 225.

- Связать campaign/ad group/ad spend с channel, account, SKU/Offer, order и
  promotion только при наличии official source или утверждённого mapping.
- Зафиксировать attribution window и policy: direct, channel-assigned, shared,
  unattributed, delayed и disputed spend.
- Обеспечить spend conservation: весь подтверждённый расход либо распределён
  по правилу, либо виден в `unattributed` с суммой/coverage.
- Не считать clicks/impressions заказами и не приписывать одному расходу два
  канала; AI может предложить mapping только как draft.
- Связать advertising/promotion facts с P&L и unit economics без double count
  spend, seller discount или marketplace subsidy.

**Acceptance:** fixtures с двумя touchpoints, несколькими SKU, shared campaign,
  delayed facts, channel-level spend и ambiguous mapping дают conservation,
  coverage и понятный attribution status.

### 227.10 — Completeness-aware calculation engine

**Зависимости:** 227.4–227.9.

- Расширить financial run так, чтобы он собирал order accrual, settlement и
  cash views из одной versioned input snapshot, не смешивая basis.
- Рассчитывать P&L, contribution profit, margin, cash, payout, COGS, FX,
  advertising/promotion и coverage с metric definition version.
- Для каждой строки хранить source refs, input digest, allocation/valuation/
  attribution policy version и value quality (`observed`, `estimated`,
  `missing`, `unmatched`, `disputed`, `stale`).
- Поддержать late facts и manual/daily rerun как новую immutable report version;
  опубликованный snapshot не переписывается.
- Сделать hard blockers для unsafe decisions и мягкие warnings для показателей,
  которые можно показать частично.

**Acceptance:** полная, частичная и конфликтная fixture-строки дают разные
quality statuses; payout не попадает в revenue, missing input не становится
нулём, а повтор run с тем же digest детерминирован.

### 227.11 — Финансовая reconciliation и очередь исключений

**Зависимости:** 227.4–227.10.

- Сверять bank ↔ payout ↔ settlement ↔ Payment, Order ↔ refund/return,
  inventory valuation ↔ COGS, FX source ↔ conversion и ads ↔ attribution.
- Использовать типизированные findings: timing, unmatched, duplicate,
  disputed, stale, missing_cogs, missing_fx, unattributed_advertising,
  cash_mismatch и source_conflict.
- Для каждого finding хранить expected/observed, source refs, amount/currency,
  owner, SLA, next action и resolution evidence.
- Добавить безопасные `retry`, `reconcile`, `resolve` и `adjust` intents;
  adjustment создаёт новую запись и approval, а не исправляет ledger на месте.
- Не закрывать finding возрастом, успешным ping или ручным изменением report
  row без подтверждения источника.

**Acceptance:** искусственно созданные расхождения попадают в нужные очереди,
  повтор reconciliation идемпотентен, а закрытие finding оставляет audit и
  исходные факты неизменными.

### 227.12 — API, UI и финансовый completeness center

**Зависимости:** 227.10–227.11.

- Добавить в «Финансовую аналитику» вкладки/блоки «P&L», «ДДС», «Выплаты»,
  «Себестоимость», «FX», «Реклама и атрибуция», «Полнота данных» и «Сверка».
- Показывать basis, reporting currency, coverage, source freshness, quality,
  missing/unmatched amounts и drill-down до source reference.
- Добавить bank statement/payout/COGS import preview, financial run retry,
  backfill status и manual attention actions с permission/approval.
- Обновить OpenAPI, generated SDK, events, cursor pagination и русские labels;
  stable technical codes сохранить.
- Ошибка загрузки/расчёта должна иметь понятное сообщение, correlation ID,
  кнопку «Повторить» и не стирать предыдущий подтверждённый snapshot.

**Acceptance:** оператор отличает неполную строку от нулевой, видит причину и
  источник, а UI/API/SDK одинаково показывают cash, payout, COGS, FX и
  attribution quality без raw bank/provider data.

### 227.13 — Security, RLS, audit, retention и controls

**Зависимости:** 227.2–227.12.

- Включить `FORCE ROW LEVEL SECURITY` для bank/source mappings, financial runs,
  valuation, FX conversion, attribution и reconciliation findings.
- Разделить read financial, import statement, adjust, approve и export
  permissions; sensitive adjustments требуют approval/four-eyes.
- Audit: actor, source, before/after digest, period, policy/version, approval,
  correlation ID и result; не писать tokens, full account numbers, raw bank
  statements, customer PII или provider responses.
- Определить retention/legal hold для bank evidence, financial snapshots,
  settlement, COGS layers, FX facts и attribution observations.
- Добавить quotas, upload limits, kill switch для imports/recalculation и
  secure cleanup временных released artifacts.

**Acceptance:** cross-tenant, permission, export, upload, append-only, secret
  redaction, retention and approval negative tests pass; отключение источника
  не удаляет уже подтверждённую финансовую историю.

### 227.14 — Demo, E2E, load и release gate

**Зависимости:** все предыдущие подзадачи.

- Добавить synthetic dataset: два канала, несколько валют, historical receipt/
  transfer, marketplace payout, bank receipt, commission, refund, COGS gap,
  CBR FX fact, campaign spend и unattributed/shared advertising.
- Пройти сценарии order accrual → settlement → bank cash → P&L/unit economics;
  отдельные partial refund, late payout, duplicate bank line, missing COGS,
  stale FX и delayed ad attribution.
- Проверить backfill, duplicate import, out-of-order webhook, crash after source
  commit, cross-tenant, approval denial, over-refund, currency mismatch и
  reconciliation resolve.
- Добавить authenticated browser E2E и Compose API/worker E2E; нагрузочный
  smoke — минимум 1 000 synthetic orders с bounded source ingestion и report
  runs.
- Для release закрыть qualification минимум одного официального bank/acquirer,
  одного marketplace payout source, FX source и advertising source; остальные
  явно остаются `not_available`/`partial`.

**Acceptance:** полная synthetic fixture даёт согласованные P&L, ДДС и
unit economics; суммы payout/cash/COGS/FX/ads conservation подтверждены;
production claim разрешён только при retained live/sandbox evidence, а
неполные источники явно видны пользователю.

## Архитектурные ограничения

- PostgreSQL и существующие Orders, Payments, Settlement, Inventory/WMS, FX,
  Campaign/Promotion и financial snapshots остаются authoritative sources;
  ClickHouse не участвует в разрешении денег, прав или completeness.
- Ledger и исходные банковские/settlement факты append-only; исправление —
  adjustment с lineage, новый report run и audit.
- Деньги — integer minor units + currency, quantity — exact decimal; FX только
  через immutable dated conversion snapshot, без implicit inversion и float.
- Core не ветвится по банку, marketplace или рекламной сети; provider-specific
  mapping остаётся за connector adapters/capabilities.
- Банковские реквизиты, токены, raw statements, raw provider payloads и лишний
  PII не попадают в обычные columns, events, logs, exports или fixtures.
- AI/MCP/n8n могут читать quality/preview и предлагать mapping, но не могут
  подтверждать adjustment, менять ledger или объявлять отчёт полным.

## Не входит в этот task

- Бухгалтерская отчётность, налоговая декларация, IFRS/РСБУ closing и payroll.
- Chargeback/dispute case management как отдельный операционный продукт.
- Подключение всех банков и рекламных сетей сразу; каждая новая интеграция
  проходит отдельную connector qualification.
- Замена P&L на ClickHouse или создание второго cash/settlement ledger.

## Зависимости

058, 059, 089, 131, 164, 167, 174, 219, 220, 221, 225, 226.

## Definition of Done

- Все 14 подзадач имеют implementation, contract/docs и success/failure/
  idempotency tests.
- P&L, ДДС и unit economics используют единые source precedence, basis,
  valuation, FX и attribution versions; payout не считается revenue.
- Для каждого периода видны coverage и неполные компоненты; zero-filling
  отсутствующих bank/COGS/FX/ad facts запрещён.
- Bank/payout/COGS/FX/advertising source qualification, RLS, audit,
  reconciliation, UI, authenticated E2E и release evidence сохранены.
- Пройдены `gofmt`, `go test ./...`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `make architecture`, `make migrations`,
  frontend typecheck/build и connector conformance на целевой topology.
