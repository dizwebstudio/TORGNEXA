# Task 165 — Прогноз остатков и автопополнение

## Status

`repository-complete` — provider-neutral foundation and the bounded operator
runtime are implemented and validated. The repository now has exact planning
contracts, tenant-scoped PostgreSQL persistence with RLS/outbox/audit, a
deterministic forecast/projection/recommendation path, REST/OpenAPI plus
generated SDK, and a frontend workspace at `/replenishment`. The current
runtime is explicitly `recommendation_only`: it does not create or submit
purchase orders and therefore cannot bypass procurement approval or write the
WMS ledger. Provider-backed input adapters, scheduled production workers,
approved draft/submit PO execution and live connector qualification remain
release gates, not hidden claims of readiness.

## Objective

## Repository completion evidence — 2026-09-01

- migration `000050_replenishment_runtime.sql` is cataloged with a verified
  digest, forced RLS, append-only controls and the immediate predecessor
  dependency;
- event `commerce.replenishment.run_completed.v1`, its schema fixture and the
  generated SDK/OpenAPI operation set are synchronized;
- `GET/POST /api/v1/replenishment` persist an immutable input digest and expose
  exact p50/p90 forecast values, projected available stock, shortfall,
  recommendation reasons and quality metadata;
- the frontend route `/replenishment` lets an operator enter bounded planning
  facts, run a preview and inspect recommendations, freshness/quality and
  reason codes. No provider payloads or secrets are shown;
- targeted Go tests, migration checks, contract validation, frontend typecheck,
  53 logic tests, public-docs checks and production build passed.

This closes the repository implementation slice. The release gates below
remain intentionally visible and must be completed with approved external
credentials, synthetic Compose evidence and current connector qualification.

Довести существующий Task 053 из простого advisory-расчёта до production-ready
контура прогноза спроса/остатков и управляемого автопополнения. Система должна
показывать прогноз по offer/SKU, складу и при необходимости каналу продаж,
дату возможного stockout, days of supply, overstock-risk, confidence и
объяснимую рекомендацию заказа.

Автопополнение должно учитывать текущий WMS ledger, reservations, quarantine,
подтверждённый inbound, purchase orders, supplier lead time, MOQ/case pack,
бюджет, складскую ёмкость, сезонность, промо и policy service level. По
умолчанию результат остаётся рекомендацией или создаёт `draft` PurchaseOrder;
отправка поставщику — отдельный approved write через существующий procurement
контур. Полностью автоматическая отправка допускается только как явно
включаемый, ограниченный и обратимо приостанавливаемый режим.

Текущий `internal/platform/replenishment` принимает только целочисленную
скорость продаж, lead time, safety stock и on-hand/reserved, считает простую
формулу `velocity × lead_time + safety`, хранит in-memory reference и жёстко
оставляет `AutoSendPO=false`. Task 165 расширяет этот baseline, не заменяя
Task 052/053, Task 054/055 WMS или canonical inventory.

## Architecture boundaries

- PostgreSQL/WMS inventory ledger остаётся источником истины по физическому
  on-hand, reserved, quarantine и движениям. Forecast и projection — derived
  planning facts; они не могут записывать остаток напрямую и не авторизуют
  отгрузку/продажу.
- ClickHouse используется для длинной истории и аналитики, но не для
  транзакционной проверки лимита или подтверждения PurchaseOrder. В PostgreSQL
  сохраняются актуальный run, snapshot digest, recommendation и execution
  evidence.
- Core остаётся provider-neutral: расчёт работает с Offer/Warehouse/Supplier/
  PurchaseOrder и capability ports, без веток по Ozon, Wildberries, 1С или
  конкретному перевозчику. Внешние остатки и продажи попадают в planning input
  только через уже квалифицированные sync/reconciliation routes.
- Существующие `replenishment_snapshots`/`replenishment_recommendations` и
  `procurement.PurchaseOrder` расширяются additive-изменениями. Нельзя создавать
  второй PO lifecycle, второй stock ledger или скрытый планировщик.
- Все мутации создают audit + Transactional Outbox в одной транзакции;
  consumers используют Inbox/deduplication. Любой worker после narrow claim
  повторно применяет tenant scope, policy, capability и version checks.
- Money — integer minor units + currency, quantity — exact decimal. Неизвестный
  или устаревший input не подменяется нулём: прогноз/рекомендация получает
  bounded quality warning и может быть запрещена к автоматическому исполнению.
- Автопополнение не является AI/MCP/n8n privileged bypass. Модель, если она
  используется, выдаёт только версионированный bounded forecast; все PO writes
  проходят обычные policy, approval, budget, audit, idempotency и procurement
  ports.

## Operating modes to approve in the ADR

Нужно зафиксировать три режима для каждой workspace/warehouse/supplier policy:

1. `recommendation_only` — строить forecast и recommendation, не создавать PO;
2. `draft_po` — идемпотентно создавать или обновлять только `draft` PO после
   preview и проверки лимитов;
3. `auto_submit` — опциональный режим для узкого allowlist: явный opt-in,
   spend/quantity/cadence caps, approved supplier/legal entity, действующий
   capability и approval/четырёхглазный контроль. Kill switch и manual pause
   обязательны.

`auto_submit` не должен следовать из одного флага рекомендации или из наличия
SDK-метода поставщика. Если policy/ADR не допускает безопасную автоматическую
отправку, worker оставляет PO в `draft` и создаёт manual attention.

## Subtasks and implementation order

### 165.1 — ADR, scope, operating modes and policy matrix

**Depends on:** none.

**Статус:** выполнено для foundation. ADR 0118 фиксирует три режима,
advisory default, fail-closed и границы WMS/procurement; полноценная policy
matrix с live capability evidence остаётся частью runtime-квалификации.

- Зафиксировать ADR, который расширяет ADR-0056, но сохраняет advisory default
  и правило «создание/отправка PO — отдельная approved write».
- Утвердить planning grain: Offer/SKU × Warehouse × optional sales channel,
  UTC bucket, forecast horizon, recompute cadence и минимальную историю.
- Описать policy по service level, safety stock, review period, target days of
  supply, stockout/overstock thresholds, MOQ, case pack, supplier priority,
  budget и warehouse capacity.
- Определить ownership входных фактов: orders/net demand, returns/cancellations,
  WMS ledger, inbound/PO, lead time, promotions и marketplace signals.
- Определить режимы `recommendation_only`, `draft_po`, `auto_submit`, approval
  classes, four-eyes, emergency pause и правила перехода между режимами.
- Зафиксировать cold-start, sparse/intermittent demand, discontinued SKU,
  supplier unavailable, stale data и negative/contradictory facts.

**Acceptance:** ADR и policy matrix одобрены; для каждой автоматической
операции указан риск, owner, лимит, approval, rollback/manual path и точный
источник входных данных; ADR-0056 не нарушен неявным auto-send.

### 165.2 — Canonical planning model and invariants

**Depends on:** 165.1.

**Статус:** выполнено для базовых контрактов и exact arithmetic. Добавлены
provider-neutral типы, digest/version lineage, quality gates, p50/p90,
shortfall и MOQ/case-pack invariants; проверки существования/архивности
канонических Offer/Warehouse/SupplierOffer будут в input/repository slice.

- Ввести provider-neutral типы `DemandObservation`, `ForecastRun`,
  `ForecastPoint`, `StockProjection`, `ReorderPolicy`,
  `ReplenishmentRecommendation`, `PurchasePlan` и `PlanningQuality`.
- Разделить фактические observation, derived forecast, projected stock,
  recommendation и execution decision; каждый output ссылается на immutable
  input snapshot digest и algorithm/model version.
- Поддержать point forecast и bounded intervals (например p50/p90), horizon,
  unit/currency, generated_at, valid_until, confidence и reason codes.
- Зафиксировать invariants: `available = on_hand - reserved` по правилам WMS;
  projected stock учитывает confirmed inbound и только допустимый demand;
  recommended quantity не отрицательна, не превышает caps и округляется к
  MOQ/case pack без потери exact quantity.
- Запретить forecast для неизвестных Offer/Warehouse/SupplierOffer, смешение
  units/currencies и использование удалённых/архивных сущностей.

**Acceptance:** domain tests покрывают fractional quantities, zero/sparse
demand, cold-start, discontinued SKU, stale/contradictory observations,
MOQ/case-pack rounding, over-capacity и no-negative-projection invariants;
provider IDs отсутствуют в Core types.

### 165.3 — Input ingestion, normalization and data-quality gates

**Depends on:** 165.1, 165.2.

- Составить bounded input adapters для order events, cancellations/returns,
  inventory/WMS ledger, reservations, quarantine, open PO/inbound shipment,
  supplier lead-time observations, promotions and approved demand signals.
- Нормализовать timezone/UTC bucket, SKU/Offer/Warehouse mapping, units,
  currency, cancellation/return netting и duplicate source events.
- Определить, какие order statuses формируют demand, как обрабатывать
  backorders, marketplace cancellations, stockout-censored sales и transfers.
- Ввести freshness/completeness/coverage checks и reason codes; при нарушении
  порога прогноз помечается `degraded`/`unavailable` и auto-submit fail-closed.
- Добавить resumable bounded backfill с watermark и digest; не загружать
  production PII и raw provider payloads в training/planning input.

**Acceptance:** fixtures на out-of-order/duplicate events, returns после sale,
stockout censoring, timezone boundary, missing warehouse, unit mismatch,
provider outage и stale source дают детерминированный quality result; один
tenant не видит входы другого.

### 165.4 — Forecast engine, baselines and uncertainty

**Depends on:** 165.2, 165.3.

**Статус:** базовый deterministic latest-observation baseline выполнен.
Backtest, сезонность, EWMA/интервалы и model registry ещё не подключены.

- Реализовать deterministic baseline первой версии: seasonal-naive/EWMA или
  иной одобренный алгоритм с явным fallback для sparse/intermittent demand;
  алгоритм и параметры версионировать.
- Рассчитывать forecast на horizon с bounded p50/p90/interval, confidence,
  sample count, missingness и explainable feature summary; не использовать
  binary float для money/quantity.
- Добавить outlier/return/promo treatment, seasonality/calendar, launch/end-of-
  life flags и separate channel aggregation без double counting.
- Поддержать model comparison/backtest: MAE/WAPE/bias, stockout recall,
  coverage of intervals и minimum sample gates; плохая модель откатывается на
  baseline и помечается в evidence.
- Не допускать online training или arbitrary user code в API/worker; модель
  загружается только из проверенного versioned registry/config.

**Acceptance:** deterministic fixtures дают стабильный digest и прогноз;
backtest/interval coverage, zero-demand, promotion spike, seasonality,
new-SKU fallback и model rollback покрыты тестами; прогноз не изменяет
inventory/procurement факт.

### 165.5 — Stock projection, risk and scenario simulation

**Depends on:** 165.3, 165.4.

**Статус:** базовая projection с inbound, no-negative clamp и явным shortfall
выполнена; days-of-supply, stockout date, overstock и сценарии остаются.

- Рассчитывать дневную/периодную проекцию: opening available + confirmed
  inbound − forecast demand − reservations/allocations с явными правилами
  lead-time uncertainty и receiving delay.
- Выводить `days_of_supply`, projected stockout date, service-level risk,
  overstock/dead-stock risk, inbound coverage и confidence degradation.
- Поддержать bounded scenarios: base, conservative/high demand, delayed inbound,
  promotion uplift; сценарий не меняет operational state и не создаёт PO.
- Учесть несколько складов, transfer candidates, quarantine/blocked lots,
  expiry/FEFO и warehouse capacity только если соответствующие WMS facts
  доступны; отсутствие данных должно быть видно оператору.
- Сохранять explanation graph/lineage references, а не повторять полный payload.

**Acceptance:** сценарии не смешивают warehouses/units/currencies, stockout и
overstock risk совпадают с synthetic ledger/inbound fixtures, partial inbound
и delayed receipt не создают отрицательный stock; расчёт bounded по offers,
horizon и memory.

### 165.6 — Reorder policy and supplier/quantity optimization

**Depends on:** 165.1–165.5.

**Статус:** базовая policy/recommendation и exact MOQ/case-pack/max-order
проверки выполнены; supplier/legal-party, budget/capacity и open-PO
оптимизация ещё не подключены.

- Реализовать reorder point/target stock: demand during lead time + safety /
  service-level buffer с учётом projected available, inbound, review cadence и
  supplier lead-time uncertainty.
- Выбирать SupplierOffer по approved supplier/legal-party, valid-until,
  currency, price, MOQ, case pack, lead time, capacity и priority; правила
  выбора должны быть объяснимыми и детерминированными.
- Поддержать split между поставщиками только при явной policy; иначе fail
  closed при конфликтующих MOQ/валютах/сроках, без скрытого greedy результата.
- Ограничить quantity budget, warehouse capacity, max order, cadence и
  working-capital policy; отложенные/уже открытые PO не дублировать.
- Сформировать recommendation с quantity, supplier, expected receipt window,
  projected risk reduction, reason codes и `eligible_mode`.

**Acceptance:** property tests подтверждают no-negative/no-over-order, MOQ/case
pack, budget/capacity, supplier validity, open-PO deduplication и stable tie
breaking; recommendation объясняет, почему quantity равна нулю или отложена.

### 165.7 — Persistence, lineage and retention

**Depends on:** 165.2–165.6.

**Статус:** additive migration 000032 с RLS, indexes, run/input digests,
append-only recommendation history и draft-plan metadata выполнена. Репозитории,
retention jobs и outbox lineage ещё не подключены.

- Additive migration расширяет `replenishment_snapshots` и
  `replenishment_recommendations` либо вводит связанные tables для forecast
  runs/points, projections, policies, quality warnings, decisions, PO links и
  execution evidence; существующие readers/writers не ломаются.
- На tenant-owned relations включить FORCE RLS, composite scope keys,
  optimistic versions и append-only history для forecast/recommendation/
  execution decisions.
- Индексы: tenant + grain + valid_at, due runs, risk/status, recommendation
  digest, supplier/warehouse, open PO idempotency и bounded operator queries;
  подтвердить планы на больших history tables.
- Писать lineage через существующий Task-030 boundary: inputs, algorithm,
  policy, output version, audit/event IDs и transformation digest.
- Операционные points/run state архивировать по retention; snapshots,
  decisions, approvals, PO/audit/financial evidence сохранять дольше и
  учитывать legal hold.

**Acceptance:** migration static, fresh install/upgrade rehearsal, RLS
cross-tenant denial, immutable digest/algorithm tests, lineage join и retention
policy проходят; ClickHouse history не участвует в authorization.

### 165.8 — Event triggers, scheduler and forecast worker

**Depends on:** 165.3–165.7.

- Добавить durable schedule через существующий Task-108 PostgreSQL-owned
  scheduler и event-triggered recompute после material order/inventory/PO/
  lead-time changes.
- Использовать debounce/coalescing по planning grain и bounded catch-up,
  чтобы burst stock/order events не создавали forecast storm на small VPS.
- Worker claims narrow tenant jobs, повторно проверяет policy/version/data
  quality, строит snapshot → forecast → projection → recommendation и
  фиксирует progress/evidence атомарно там, где это возможно.
- Retry только для retry-safe DB/queue операций с jitter; provider/network
  failure остаётся quality warning или account-local retry, а не блокирует
  другие tenants.
- Inbox/dedup и deterministic run identity гарантируют, что повторный event или
  crash после сохранения результата не создаёт второй logical run.

**Acceptance:** duplicate event, scheduler restart, lease loss, crash между
snapshot/forecast/recommendation, clock skew, bounded catch-up и provider
outage не дают duplicate runs; lag и stale forecast наблюдаемы.

### 165.9 — Draft PO and guarded auto-replenishment execution

**Depends on:** 165.6–165.8.

- Для `recommendation_only` не создавать PO; для `draft_po` создавать
  tenant-scoped `PurchaseOrder` в статусе `draft` с link на recommendation,
  snapshot, supplier offer и deterministic idempotency key.
- Перед каждым draft/submit перечитать inventory, open PO, supplier offer,
  budget, legal entity, workspace mode, approval, capability и current policy;
  stale recommendation должна быть пересчитана или заблокирована.
- Использовать существующий procurement PO lifecycle (`draft -> approved ->
  sent`) и Task-017 approval; не добавлять worker-only shortcut `draft -> sent`.
- `auto_submit` разрешать только по allowlist supplier connector с
  `purchase_order.write`, dry-run/preview, remote idempotency, timeout,
  reconciliation и explicit spend/quantity/cadence caps. Unknown outcome
  переводить в manual attention, не повторять вслепую.
- Добавить per-tenant kill switch, daily/monthly budget, max SKU fan-out,
  circuit breaker и safe rollback: уже отправленный PO не отменяется молча,
  correction идёт через обычный procurement/approval path.

**Acceptance:** E2E synthetic run показывает recommendation-only, idempotent
draft creation, approval-required hold, successful submit, budget/capacity
block, stale-input block, duplicate worker, provider timeout/accepted-unknown
и kill switch; ни один запуск не создаёт два PO или не обходит PO state machine.

### 165.10 — REST/OpenAPI, MCP/n8n boundary and operator UI

**Depends on:** 165.2, 165.5–165.9.

- Добавить tenant-scoped API для policy CRUD/preview, forecast run/status,
  forecast/risk list, scenario preview, recommendation accept/reject/defer,
  draft PO preview/create, auto-replenishment enable/pause и bounded execution
  history.
- Все mutations требуют `Idempotency-Key` и optimistic version; tenant/
  workspace берутся из auth context; списки используют cursor pagination.
- Обновить OpenAPI, generated SDK, permission matrix, events и MCP/n8n
  contracts; MCP/n8n получают read/preview или обычный approved mutation, но
  не прямой access к model files, secrets или SQL.
- В UI добавить раздел «Прогноз и пополнение»: график demand/stock/inbound,
  stockout/overstock risk, confidence/data freshness, explanation, scenario,
  supplier/MOQ/cost, recommendation actions и mode/limit status.
- Действия «Создать черновик», «Отправить на согласование» и «Автопополнение»
  должны различаться визуально; auto-submit скрыт/disabled без capability,
  policy, approval и current qualification. Показать stale/manual attention и
  ссылку на lineage/audit evidence.

**Acceptance:** оператор может выполнить preview, понять источник прогноза,
создать ровно один draft PO, пройти approval и поставить policy на pause;
UI/API не показывают фиктивные supplier capabilities и не раскрывают raw
provider/model payload.

### 165.11 — Connector, supplier and inbound qualification

**Depends on:** 165.6, 165.9, 165.10.

- Составить capability matrix для supplier/ERP/marketplace inputs:
  `orders.read`, `inventory.read`, `inbound.read`, `supplier_offer.read`,
  `purchase_order.create/submit`, status/reconciliation и webhook.
- Разделить SDK manifest, host runtime route, test sandbox и Docker/live
  evidence; manifest или health-only card не разрешает auto-submit.
- Для каждого admitted source проверить mapping SKU/Offer/Warehouse, freshness,
  rate limits, pagination, remote idempotency, dry-run, timeout и ambiguous
  outcome. Неподдержанное поле не заполнять догадкой.
- Подготовить synthetic connectors/stubs для supplier quote, lead-time update,
  delayed inbound, partial receipt, rejected PO, 429 and accepted-but-lost
  response.
- Выпустить capability snapshots, qualification report и rollback rule; до
  evidence оставлять режим только `recommendation_only`/`draft_po`.

**Acceptance:** generated catalog/runtime support/conformance agree; каждый
`auto_submit` provider имеет current idempotent end-to-end evidence, а SDK-only
или health-only provider fail-closed в API, UI, worker и automation builder.

### 165.12 — Security, observability, quotas and recovery

**Depends on:** 165.7–165.11.

- Метрики: forecast freshness/quality, run lag, horizon coverage, WAPE/MAE/bias,
  stockout recall, interval coverage, recommendation age, draft/submit count,
  approval wait, budget blocks, duplicate suppression, provider latency/rate
  limit, DLQ and per-tenant saturation.
- Logs/traces содержат только tenant/workspace/offer/warehouse/run/
  recommendation/PO IDs, digests, policy/model version и bounded error codes;
  PII, secrets, model credentials и raw supplier payload redacted.
- Лимиты workspace: offers/run, horizon, history, concurrent runs, events/min,
  max fan-out, POs/day, spend/day/month, remote calls, retries, DB rows and
  memory. Quota breach должен быть tenant-local.
- Подготовить runbook для stale forecast, data-quality incident, overstock,
  stockout, duplicate PO, supplier outage, unknown remote outcome, budget
  breach, runaway model version и emergency pause.
- Провести threat model: cross-tenant SKU injection, replay, duplicate spend,
  unauthorized mode escalation, poisoned demand/returns, SSRF через supplier
  endpoint, artifact/PII leakage и AI/MCP privilege escalation.

**Acceptance:** dashboards/alerts различают unavailable/degraded/healthy
forecast, recommendation hold, approval wait, provider outage и submit unknown;
kill switch/quota не затрагивает другие tenants, recovery не требует ручного
изменения ledger/PO фактов SQL-командой.

### 165.13 — Tests, Compose qualification and documentation

**Depends on:** all previous subtasks.

- Добавить Go unit/property/integration tests для decimal/money, forecast
  baselines, intervals, projections, policy, supplier selection, repositories,
  RLS, lineage, idempotency, API, scheduler, worker и PO transitions.
- Провести Compose E2E: synthetic orders/inventory/WMS/PO/inbound → outbox →
  Kafka → forecast worker → risk/recommendation → draft PO → approval →
  qualified supplier stub → receipt/reconciliation.
- Добавить crash/chaos cases: Kafka redelivery, PostgreSQL restart, worker
  crash at every persistence/submit boundary, delayed inbound, duplicate webhook,
  provider 429/5xx, model rollback and budget/circuit-breaker trips.
- Выполнить load profile для 10k/100k offers, event burst, daily recompute,
  long horizon and supplier fan-out; подтвердить bounded memory, DB pool,
  ClickHouse ingest and Kafka lag на small-VPS Compose.
- Обновить docs/38, operations/053, contract/OpenAPI/event docs, runbook,
  `.env` reference, UI screenshots и capability matrix; сохранять synthetic
  qualification evidence и algorithm/model digests.

**Acceptance:** проходят `go test ./...`, `go vet ./...`, contracts,
architecture, migrations, frontend, conformance, performance и Compose E2E;
документация соответствует фактическим режимам, а `auto_submit` не считается
production-ready без retained qualification evidence.

## Suggested delivery slices

1. **Planning foundation:** 165.1–165.4 — ADR, model, data quality and
   deterministic forecast baseline.
2. **Risk and recommendation:** 165.5–165.7 — projections, reorder policies,
   supplier optimization, persistence and lineage.
3. **Guarded execution:** 165.8–165.10 — scheduler/worker, draft/approved PO
   flow, guarded auto-submit, API and UI.
4. **Production qualification:** 165.11–165.13 — connector evidence,
   observability/security, load/chaos/Compose, screenshots and docs.

## Explicit exclusions

- forecast is not operational stock truth and cannot write `on_hand`,
  `reserved`, quarantine or WMS ledger directly;
- black-box/unversioned ML, arbitrary Python/Go/user code, LLM-generated PO
  commands and AI/MCP/n8n authority bypass;
- automatic PO submission by default, blind retry after unknown supplier result,
  silent PO cancellation or rewriting procurement/financial facts;
- cross-currency optimization without explicit sourced FX and rounding policy,
  supplier selection from unapproved legal parties, and unbounded split-order
  optimization;
- using ClickHouse, cache, Kafka delayed topics or browser state as the source
  of transactional planning/execution truth;
- enabling `auto_submit` for an SDK-only, health-only or stale/unqualified
  connector; chargebacks, returns/refunds and disputes remain separate domain
  tasks and feed planning only through canonical events.

## References

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/02-domain-model.md`
- `docs/03-module-boundaries.md`
- `docs/04-event-platform-kafka.md`
- `docs/05-database.md`
- `docs/12-reporting-bi.md`
- `docs/29-data-lineage.md`
- `docs/38-procurement-supply-planning.md`
- `docs/39-wms.md`
- `docs/46-sre-performance-slo.md`
- `docs/54-architecture-freeze-v1.md`
- `docs/operations/053-demand-replenishment.md`
- `contracts/operations/053-demand-replenishment-v1.md`
- `adr/0009-transactional-outbox.md`
- `adr/0018-slo-performance.md`
- `adr/0041-approval-engine-policy-evidence.md`
- `adr/0052-clickhouse-reporting-projections.md`
- `adr/0055-procurement-core.md`
- `adr/0056-replenishment-planning.md`
- `adr/0057-wms-inventory-ledger.md`
- `adr/0058-wms-execution.md`
- `tasks/issues/052-procurement-core.md`
- `tasks/issues/053-demand-and-replenishment-planning.md`
- `tasks/issues/054-wms-inventory-ledger.md`
- `tasks/issues/055-wms-execution.md`
- `tasks/issues/108-connector-bootstrap-sync.md`
- `tasks/issues/163-workflow-automation-builder.md`
