# Task 167 — Юнит-экономика по каналам

## Статус

`repository-complete` — реализованы provider-neutral fixed-decimal engine,
tenant-scoped channel attribution/cost/run metadata with forced RLS,
settlement-kind expansion and deduplication, factual PostgreSQL report
`unit_economics_by_channel`, OpenAPI/SDK filters, operator UI and contract/
architecture/migration test evidence. ClickHouse projection and live source
watermarks remain disposable/release-topology concerns; the report fails closed
and marks missing COGS, FX and attribution instead of zero-filling them.

## Цель

Дать оператору воспроизводимый фактический расчёт юнит-экономики по каждому
каналу продаж: marketplace, интернет-магазин, ERP/касса, social/affiliate и
другие каналы, для которых есть подтверждённые источники фактов. Расчёт должен
показывать не только оборот, но и вклад канала после скидок, возвратов,
комиссий, эквайринга, логистики, хранения, рекламы, себестоимости и прочих
переменных расходов.

Это не продолжение what-if сценария из `profitability-v1`: существующий
`POST /profitability/scenarios` остаётся совместимым инструментом ручного
моделирования. Task 167 добавляет фактические, версионированные и
доказуемые snapshots по каналу, заказу, Offer/SKU и периоду, не превращая
ClickHouse в финансовый ledger и не создавая второй Product/Order/Settlement
источник истины.

## Текущий разрыв

- В отчётах есть только `sales_daily`, `inventory_current` и
  `ingestion_freshness`; фактической агрегации contribution profit по каналам
  нет.
- `trustcontrol` умеет посчитать упрощённый fixed-decimal сценарий, но он
  принимает введённые пользователем суммы и не связывает их с Order,
  SettlementEntry, Payment, Return, Campaign или COGS.
- Заказы знают коммерческий snapshot и суммы, а remote/channel identity
  должна разрешаться через connector mapping; единого аналитического
  `channel_ref` и правил атрибуции нет.
- Settlement ledger append-only, но в документации заявлены logistics,
  storage, advertising, penalty, compensation и withholding, тогда как
  текущая Go-модель `settlements.Kind` допускает более узкий набор. Нельзя
  молча считать отсутствующие типы нулевыми: контракт/импорт должен быть
  расширен отдельной совместимой задачей внутри 167 или явно помечать расход
  как `unsupported`.
- В OrderItem нет исторического snapshot себестоимости. Использование текущего
  `Price(kind=cost)` для старого заказа без as-of политики даст неверную
  прибыль и должно быть запрещено.
- События заказов, settlement и FX уже проходят через Outbox/Kafka и
  ClickHouse projection, но нет расчётного run с digest входов, completeness и
  объяснением распределения общих затрат.

## Термины и границы, которые должны быть утверждены в ADR

### Канал

`channel_ref` — provider-neutral стабильная ссылка на tenant-scoped канал,
состоящая из `connector_account_id` и, если он существует, remote store/listing
или channel mapping. Имя провайдера не является бизнес-ключом. Один connector
account может иметь несколько магазинов/витрин; они не должны сливаться в одну
строку без явного mapping.

Если заказ или расход нельзя однозначно связать с каналом, разрешено только
отдельное ведро `unattributed` с причиной и coverage-метрикой. Нельзя
раскладывать неизвестный факт по каналам пропорционально «для красоты».

### Базы признания

В каждом запросе и snapshot обязателен один basis:

- `order_accrual` — дата коммерческого факта заказа/строки;
- `settlement` — дата факта marketplace/acquirer settlement;
- `cash` — дата подтверждённой выплаты/банковского receipt.

По умолчанию UI использует `order_accrual`, но не смешивает его с settlement
или cash. Отдельный reconciliation показывает разницу между basis, а не
подменяет одну базу другой. Все границы периода и timestamps — UTC.

### Денежная формула

Предлагаемая версия `channel-unit-economics-v1` должна закрепить знаки и
порядок расчёта:

```text
gross_merchandise_value
- discounts
- cancellations
- refunds_and_returns
= net_merchandise_sales

net_merchandise_sales
- tax_pass_through (если политика исключает налог из выручки)
- marketplace_commission
- payment_processing_fee
- fulfilment_and_delivery
- storage
- advertising_spend
- promotion_subsidy
- cogs
- penalties
+ compensation
= contribution_profit

contribution_margin_bps = contribution_profit * 10_000 / net_revenue
```

Формула не фиксирует бухгалтерскую выручку автоматически: ADR должен выбрать
режим `gross`/`net`, включение доставки и налога, а также уровни `CM0` (после
скидок/возвратов), `CM1` (после COGS и channel fees) и `CM2` (после рекламы,
логистики и прочих переменных затрат). Каждый показатель обязан иметь
`metric_definition_version`, `sign_policy` и источник.

`payout` никогда не прибавляется к выручке, если тот же sale уже учтён из
Order/Settlement. Выплата используется для cash view и reconciliation.

## Архитектурные ограничения

- PostgreSQL, Orders, OrderItems, Prices/COGS, Payments, Returns/Refunds,
  SettlementEntry, Campaign/Promotion и canonical FX facts остаются
  authoritative sources. ClickHouse — только disposable analytical projection.
- Исходные ledger facts append-only; исправление — новый adjustment с ссылкой
  на исходную запись. Расчётная строка также immutable для конкретного run и
  входных digest; пересчёт создаёт новую версию.
- Все суммы — `int64 minor_units + ISO currency`; количества — exact decimal.
  Запрещены `float64`, бинарное округление и silently mixed currencies.
- Cross-currency totals разрешены только через Task 089b immutable conversion
  snapshot с `as_of`, source/rate/policy reference и округлением в самом конце.
  Missing/stale/ambiguous FX делает строку `incomplete`, а не нулевой.
- Core и расчётный движок не ветвятся по `ozon`, `wb` и другим именам. Различия
  описываются channel/connector capability, mapping и versioned policy.
- Ни quality report, ни AI/MCP/n8n не могут менять Order, Price, Inventory,
  SettlementEntry или подтверждать финансовую операцию.
- В событиях, ClickHouse и report response нет токенов, raw provider payloads,
  полных банковских реквизитов и лишнего PII; сохраняются только bounded refs,
  hashes, amounts и machine reason codes.

## Состав фактов и приоритет источников

ADR должен утвердить матрицу `metric -> canonical source -> fallback ->
quality status`:

| Факт | Предпочтительный источник | Запрещённая подмена |
|---|---|---|
| продажа/строка/quantity | immutable Order/OrderItem и order event | сумма payout вместо продажи |
| discount/tax/shipping | Order snapshot или проверенный channel fact | вывод из одной grand total без policy |
| commission/logistics/storage/penalty | SettlementEntry нормализованного типа | текущий тариф из connector без факта |
| payment fee | Payment commission или settlement fee с dedup evidence | двойной учёт обоих источников |
| refund/return/cancellation | Task 164 facts и payment refund evidence | отрицательный заказ или ручное удаление sale |
| advertising/promotion | Campaign/AdGroup spend и attribution evidence | вся реклама workspace на каждый канал |
| COGS | исторический cost snapshot/inventory valuation policy | текущая цена cost без as-of |
| cash/payout | settlement payout/bank receipt | считать payout выручкой |
| FX | Task 089b ConversionSnapshot | live spot/cache без evidence |

Для каждого output сохраняются `source_refs`, `input_digest`, `allocation_rule`
и `quality_status`. Наличие только части фактов видно пользователю и влияет на
coverage, но не маскируется нулём.

## Подзадачи и порядок реализации

### 167.1 — ADR, словарь, scope и accounting policy

**Зависит от:** нет.

- Зафиксировать ADR о channel unit economics и не смешивать management
  contribution margin с бухгалтерским P&L, налоговым учётом и cash-flow.
- Утвердить определения channel, order/settlement/cash basis, GMV, gross/net
  revenue, COGS, CM0/CM1/CM2, contribution profit, margin, take rate, CAC,
  ROAS/ROMI, refund rate и coverage.
- Выбрать знак каждой `SettlementEntry.kind`, правила включения tax/shipping,
  компенсаций, chargeback/penalty и дата-правило для возврата, отмены и
  settlement adjustment.
- Описать минимальный набор v1 и deferred metrics; неизвестное должно иметь
  отдельный статус, а не считаться нулём.
- Согласовать связи с Tasks 006, 049, 050, 058, 059, 061, 089 и 164, а также
  legal entity/counterparty и retention policy.

**Приёмка:** ADR содержит формулы, примеры знаков и два worked examples
(полный заказ и частичный refund); finance/product review подтверждает, что
одна и та же продажа не учитывается дважды через Order + settlement payout.

### 167.2 — Каноническая модель канала и атрибуции

**Зависит от:** 167.1.

- Ввести provider-neutral `ChannelRef`/`ChannelDimension` с tenant scope,
  store/business unit, connector account, remote listing reference, family,
  status, currency policy и effective/version timestamps.
- Разрешать channel только через connector entity mapping, sync policy или
  явный order/campaign attribution; не принимать `provider` из свободного
  пользовательского payload как доверенную связь.
- Поддержать assignment states `resolved`, `unattributed`, `ambiguous`,
  `retired`; retired channel остаётся историческим измерением.
- Добавить deterministic resolution order и reason codes для заказа, ledger
  entry, payment, campaign, return и delivery cost.
- Зафиксировать backfill/merge policy: исправление атрибуции создаёт новую
  аналитическую версию и lineage, не переписывает исходный факт.

**Приёмка:** одинаковая tenant-scoped ссылка стабильно разрешается при replay;
foreign/ambiguous mapping не попадает в канал; drill-down показывает причину
`unattributed` и процент покрытия.

### 167.3 — Контракты метрик, знаков и качества данных

**Зависит от:** 167.1–167.2.

- Создать Draft 2020-12 schema для `ChannelUnitEconomicsRow` и
  `ChannelUnitEconomicsSnapshot` с exact money, quantity, period, basis,
  channel_ref, metric version, input digest, coverage и evidence refs.
- Описать typed components: revenue, discounts, taxes, shipping, COGS,
  commissions, payment fees, logistics, storage, ads, promotions, penalties,
  compensation, refunds, payout, contribution and margin.
- Не разрешать nullable/zero ambiguity: для каждого компонента хранить
  `value_status = observed | estimated | missing | not_applicable | disputed`
  и bounded `reason_code`.
- Определить статусы строки `complete`, `partial`, `stale`, `unmatched`,
  `conflict`, `mixed_currency`, `unsupported`; `numeric_score` не снимает
  hard quality status.
- Утвердить compatibility policy для additive metrics и новую версию при
  изменении формулы/знаков.

**Приёмка:** schema validator отвергает float, отрицательную quantity,
невалидную валюту, смешанный basis и неполный evidence; fixtures содержат
полную, частичную и конфликтную строки.

### 167.4 — Нормализация и приём исходных фактов

**Зависит от:** 167.2–167.3.

- Сделать read ports для Orders/Items, SettlementEntry, Payment/Refund,
  Campaign/Promotion, logistics/fulfilment, cost/valuation и FX snapshot.
- Добавить event adapters для существующих `commerce.orders.order_changed.v1`,
  `finance.settlement_entry.created.v1`, payment/refund/return/ads facts и
  новых versioned событий только через canonical envelope.
- Ввести normalized fact record с source system/ref, occurred/effective date,
  original currency, signed amount, channel resolution and source digest.
- Enforce provider-reference idempotency, event version monotonicity,
  duplicate/collision detection, late-arriving facts и bounded correction
  handling.
- Расширить settlement kind contract для logistics/storage/advertising/
  penalty/compensation/withholding только additive migration/ADR; до этого
  неизвестный kind — `unsupported`, не zero.

**Приёмка:** replay одной страницы не меняет semantic totals; duplicate
provider reference создаёт одну нормализованную запись; malformed/unknown
fact попадает в quality queue с machine code без raw provider text.

### 167.5 — Историческая себестоимость и valuation policy

**Зависит от:** 167.1, 167.3, Tasks 005/053/054.

- Утвердить source hierarchy для COGS: order-time cost snapshot, inventory
  ledger valuation, standard cost, supplier cost или `missing`.
- Реализовать exact decimal allocation на OrderItem/offer/warehouse с выбором
  FIFO/weighted-average/standard-cost как versioned policy; не смешивать
  политики внутри одного snapshot.
- Хранить `cost_as_of`, cost currency, quantity basis, source/version and
  valuation digest; изменения каталожной себестоимости не переписывают прошлый
  расчёт.
- Разделить estimated COGS и observed COGS; показать variance и не разрешать
  estimated value выглядеть как подтверждённый ledger fact.
- Учесть cancelled-before-fulfilment, partial fulfilment, returns disposition,
  quarantine/scrap и replacement без изменения исходного OrderItem.

**Приёмка:** один SKU с двумя партиями и partial return даёт ожидаемый COGS
для каждой policy; отсутствующий as-of cost даёт `missing`, а не текущую цену;
количество COGS не превышает проданное/принятое по правилам.

### 167.6 — Allocation engine общих расходов и доходов

**Зависит от:** 167.2–167.5.

- Описать детерминированные allocation keys: order, order line, offer/SKU,
  channel, shipment, warehouse, campaign и period.
- Закрепить правила распределения shipping/fulfilment/return logistics,
  storage, order-level commission, fixed payment fee, promotion subsidy и
  campaign spend: units, merchandise value, weight/volume, provider share или
  explicit manual allocation.
- Для каждого распределения сохранять numerator/denominator, rounding mode,
  residual recipient, policy version and allocation digest.
- Проверять conservation: сумма распределённых компонентов равна исходной
  сумме в той же валюте с единственным документированным rounding residual.
- Не распределять unresolved/ambiguous costs автоматически; выводить их в
  `unattributed`/`unallocated` с coverage.

**Приёмка:** property tests доказывают conservation и отсутствие отрицательных
остатков при любом порядке строк; повторный расчёт даёт тот же digest;
allocation policy change создаёт новую версию, не мутируя старую.

### 167.7 — Settlement/payment deduplication и reconciliation

**Зависит от:** 167.4, 167.6, Tasks 058–059.

- Связать settlement entries с Order/Payment/Return/Campaign через
  tenant-scoped refs и mapping, сохраняя original provider reference.
- Ввести source precedence/dedup policy для commission/payment fee,
  sale/refund/payout и adjustments, чтобы один расход не пришёл дважды из
  Payment и Settlement.
- Использовать Task-059 difference classes: timing, known_fee, unmatched,
  duplicate, disputed; reconciliation не исправляет ledger и не меняет report
  row задним числом без нового run.
- Показывать expected/observed/settled/cash views и delta to provider/bank;
  payout использовать только в cash basis.
- Поддержать late settlement, adjustment chain, disputed entry и remote
  correction с audit/lineage.

**Приёмка:** fixtures с дублированной комиссией, payout до settlement,
adjustment и disputed refund классифицируются предсказуемо; итог contribution
не завышается и reconciliation evidence содержит обе стороны сравнения.

### 167.8 — Реклама, промо и attribution

**Зависит от:** 167.2, 167.6–167.7, Task 050.

- Связать Campaign/AdGroup spend и Promotion/Discount с каналом и периодом
  только при наличии attribution evidence (`source/medium/campaign`, click or
  order link, connector account или approved rule).
- Разделить direct attributed spend, channel-assigned spend, shared spend,
  unattributed spend и promo subsidy; показывать confidence/coverage.
- Не считать impressions/clicks заказами и не смешивать ROAS/ROMI с
  contribution margin; определить denominator и attribution window.
- Расходы по рекламе и bid/budget actions остаются write-sensitive и не могут
  запускаться из отчёта; AI может предложить объяснение, но не изменить
  attribution.

**Приёмка:** один заказ с двумя touchpoints не удваивает revenue; expired или
  ambiguous attribution не попадает в direct channel profit; spend conservation
  и attribution-window tests проходят.

### 167.9 — Возвраты, отмены, refunds и shipping/tax treatment

**Зависит от:** 167.4–167.8, Task 164.

- Подключить отдельные cancellation/return/refund facts без переписывания
  immutable Order/OrderItem/Payment/Settlement.
- Поддержать partial/full return, несколько refunds, cancelled-before-shipment,
  shipping refund, tax refund, restock/quarantine/scrap и replacement согласно
  policy.
- Разделить refund amount, returned COGS, reverse commission, reverse ad/promo,
  return logistics и provider compensation; не считать refund дважды через
  payment и settlement.
- Выбрать order-date против event-date allocation для каждого basis и показать
  pending/unknown outcome до reconciliation.
- Реализовать no-over-refund/no-over-return checks и correction snapshots.

**Приёмка:** partial return изменяет net sales и contribution ровно на
  allocated quantity/amount; timeout refund остаётся `unknown`, а не ложным
  расходом/доходом; старый snapshot воспроизводится после нового возврата.

### 167.10 — FX, валюты и воспроизводимое округление

**Зависит от:** 167.3–167.9, Task 089b.

- Для каждой строки выбрать reporting currency или оставить original-currency
  bucket; не смешивать валюты в одной сумме без explicit conversion snapshot.
- Резолвить historical `as_of` rate по occurred/effective policy, сохранять
  полный FX reference, source, rate type, policy, rounding and conversion digest.
- Разрешить только прямую/утверждённую triangulation из Task 089b; missing,
  stale, inverse-only или conflicting rate дают `mixed_currency`/`incomplete`.
- Округлять только на финальном minor-unit output, хранить residual и
  проверять знак/overflow через big integer/fixed decimal arithmetic.
- Дать режимы `original`, `workspace_reporting_currency`, `compare_currency`
  без silently changing historical results when current rates update.

**Приёмка:** cross-currency fixture не складывается без evidence; один и тот же
  snapshot даёт один результат после cache refresh; missing/stale FX виден в
  UI/export и не заменяется нулём.

### 167.11 — Deterministic calculation engine и immutable runs

**Зависит от:** 167.3–167.10.

- Реализовать pure calculation pipeline: normalize → resolve channel →
  allocate → convert → aggregate → validate quality.
- Версионировать `algorithm_version`, `metric_definition_version`,
  `allocation_policy_version`, `valuation_policy_version`, `attribution_policy`
  и набор входных event/fact digests.
- Сохранять `calculation_run` с requested/effective range, basis, currency,
  source watermarks, profile/policy versions, generated_at, row count,
  complete/partial status и deterministic run key.
- Разделить `preview`, `rebuild`, `published` и `superseded`; published snapshot
  immutable, correction — новый run. Concurrent runs coalesce safely.
- Validate totals against source ledgers and expose explain trace for each row:
  components, allocations, evidence and missing facts.

**Приёмка:** identical input snapshot produces identical row digest and totals;
  changed formula or late event cannot mutate old run; overflow, negative
  denominator, mixed basis and unbounded range fail closed.

### 167.12 — ClickHouse projection, schema, backfill и freshness

**Зависит от:** 167.11, Task 049/052.

- Добавить versioned derived tables for channel/day/currency/basis, channel/
  offer detail and quality/coverage; partition/order by tenant, period,
  channel_ref, currency, basis.
- Use aggregate states/dedup keyed by event/fact id and run version; replay
  and backfill must not inflate sales, costs or refunds.
- Keep minimized analytical payloads; no raw provider documents, secrets or
  full customer PII. Store source refs/digests and bounded component columns.
- Extend `reporting.QueryPort` with bounded filters: basis, from/to, channel,
  currency, SKU/offer, status, completeness and cursor/limit.
- Expose source watermarks, ClickHouse lag, run age and ledger-vs-projection
  variance. CH outage affects reports only, never transactional writes.
- Add replay runbook and coordinated Task-061 retention/deletion propagation.

**Приёмка:** full rebuild from PostgreSQL/events equals incremental result;
  duplicate/out-of-order events are stable; query plans stay bounded for
  maximum date range and tenant; CH can be dropped/rebuilt without losing
  authoritative evidence.

### 167.13 — PostgreSQL metadata, RLS, lineage и retention

**Зависит от:** 167.2, 167.11–167.12.

- Add expand-only tables for channel dimension/mapping, calculation runs,
  snapshot metadata, quality issues, allocation evidence and report bookmarks;
  analytical rows remain in ClickHouse where appropriate.
- Force RLS on all tenant-owned metadata, use composite organization/workspace
  keys, optimistic versioning and indexes for due runs/channel/date/status.
- Bind every run/result to Task-030 lineage and Task-003 audit with correlation,
  causation, actor and policy digests; no raw payload copies.
- Define retention classes: financial/settlement evidence and published
  snapshots are retained according to policy/legal hold; transient calculation
  queue can expire only after durable evidence.
- Register migration catalog, backup checkpoint and upgrade/rollback behavior;
  rollback disables readers/writers without deleting financial history.

**Приёмка:** cross-tenant reads/writes fail under forced RLS; lineage points to
  exact source refs and input digest; retention/legal hold tests never remove
  required financial evidence; migration static/upgrade rehearsal passes.

### 167.14 — Worker, scheduler, events и recovery

**Зависит от:** 167.4, 167.11–167.13.

- Add canonical `finance.unit_economics.snapshot_requested.v1` and
  `finance.unit_economics.snapshot_published.v1` (or approved equivalent) with
  full envelope, run key, basis/range and digest — never payload dumps.
- Consume source changes through Outbox/Kafka/Inbox; coalesce bursts by tenant,
  channel and date window, while preserving a durable list of dirty ranges.
- Use PostgreSQL leases, bounded retries/jitter, retry/DLQ classification,
  checkpointed backfill and deterministic idempotency keys. A crash after CH
  insert must not duplicate semantic totals.
- Recheck policy, source watermarks, FX freshness and report permissions at
  execution; stale/unknown dependencies produce a non-published run.
- Provide operator replay, cancel, supersede and recovery commands with audit;
  never mark a failed run complete because a worker timed out.

**Приёмка:** duplicate/out-of-order events, lease loss, crash before/after
  projection, CH outage, DLQ replay and late settlement all converge to one
  correct published run; no retry storm on the small-VPS Compose profile.

### 167.15 — REST/OpenAPI, exports и permission model

**Зависит от:** 167.11–167.14.

- Extend `/api/v1/reports` catalog with a truthful
  `unit_economics_by_channel` report and a bounded detail/quality report only
  when their runtime query adapters are wired.
- Define filters `basis`, UTC `from/to`, channel_ref, connector account,
  store, currency, SKU/offer, completeness, attribution and settlement state;
  enforce maximum period, rows, component count and query timeout.
- Return columns with labels, metric/version, source, generated_at, watermarks,
  `coverage`, `quality_status`, `currency` and explanation/evidence links.
- Add CSV/PDF exports with the exact filters and snapshot/run id; exports must
  not silently recalculate using a newer run.
- Permission `reports.read` is required; financial detail may require a
  separate `finance.unit_economics.read` capability and admin-only rebuild.
  Tenant scope always comes from auth context.
- No POST endpoint may mutate ledger or launch external spend/payment actions;
  privileged rebuild is idempotent, bounded and audited.

**Приёмка:** OpenAPI, route registry and generated SDK are in parity; invalid
  basis/range/currency/cursor returns RFC7807; CSV/PDF reproduces JSON snapshot;
  unauthorized tenant/detail access is denied and no secrets appear in errors.

### 167.16 — UI «Юнит-экономика по каналам»

**Зависит от:** 167.15.

- Добавить отдельный экран в Reports с выбором basis, периода, reporting
  currency, channel/store, completeness и детализации.
- Показать marketplace/store comparison table: net revenue, orders, units,
  COGS, commission, payment fee, logistics, storage, ads, refunds,
  contribution profit, margin %, take rate, payout/cash delta и coverage.
- Добавить waterfall для выбранного канала, drill-down до order/Offer/SKU и
  side-panel «Почему так посчитано» с components, allocation rule, FX ref,
  source watermarks и missing/conflict reasons.
- Отдельно визуализировать `complete`, `partial`, `stale`, `unmatched`,
  `mixed_currency`, `disputed`; не красить unknown в зелёный и не показывать
  `0 ₽`, если факта нет.
- Добавить compare-period, sorting/pagination, no-data/loading/error states,
  CSV/PDF export and accessible keyboard/table semantics. Не тащить большую
  chart dependency без ADR; использовать текущие UI primitives.
- Кнопки «пересчитать»/«исправить атрибуцию» показывать только при capability;
  remediation открывает approval workflow, а не выполняется из браузера.

**Приёмка:** browser/visual tests проверяют русские labels, отсутствие
  overflow/перекрытий, tenant-safe drill-down, сохранение filters при export и
  понятное предупреждение о неполных данных; screen не утверждает прибыль при
  неизвестном COGS/FX.

### 167.17 — Security, privacy, observability и quotas

**Зависит от:** 167.13–167.16.

- Провести threat model для financial-report disclosure, channel enumeration,
  export abuse, cross-tenant joins, untrusted attribution text и prompt/AI
  leakage.
- Enforce RLS, `reports.read`/finance permission, no-store for sensitive
  responses, bounded exports, rate limits and maximum concurrent runs per
  workspace.
- Redact provider/customer payloads, URLs, credentials and bank/payment data;
  only machine codes/digests in logs, audit, Kafka and DLQ.
- Metrics: calculation lag, run duration, rows/bytes, source freshness,
  coverage, unmatched/ambiguous/mixed-currency counts, reconciliation delta,
  duplicate suppression, FX failures, CH query latency/error and export count.
- Alerts/runbooks for stale data, rising unattributed share, ledger variance,
  failed FX, stuck leases, DLQ age, query saturation and suspected double count.
- Provide workspace kill switch for scheduled rebuild/export storm; it must not
  block ordinary Orders/Payments commits.

**Приёмка:** security tests reject cross-tenant/channel probing and oversized
  ranges; logs contain no secrets/PII; quotas stay bounded under concurrent
  rebuilds; alerts link to actionable recovery steps.

### 167.18 — Tests, demo data, Compose и документация

**Зависит от:** 167.1–167.17.

- Unit/property tests for money arithmetic, margin/denominator, sign policy,
  allocation conservation, COGS valuation, FX rounding, dedup and quality
  statuses.
- Contract/schema tests for every event/API/report response; OpenAPI/runtime
  parity and generated SDK checks.
- PostgreSQL integration/RLS/migration tests; ClickHouse projection/replay/
  backfill tests; Inbox/Outbox and worker crash/lease/retry/DLQ tests.
- Scenario fixtures: two marketplaces + web store, mixed currencies, order
  cancellation, partial return/refund, payment fee duplicate, settlement
  adjustment, ad attribution, shared logistics, missing COGS, stale FX,
  unmatched channel and disputed entry. All data synthetic and tenant-isolated.
- Compose E2E for API → PostgreSQL/Outbox → Kafka → worker → ClickHouse → UI,
  including rebuild after dropping derived projection and export evidence.
- Update public docs with formulas, signs, bases, source matrix, freshness/
  coverage caveats, channel mapping, COGS/FX policy, troubleshooting and
  screenshots of complete/partial/mixed-currency states.
- Run `gofmt`, `go test ./...`, `go vet ./...`, contract/architecture/migration,
  frontend, Compose and bounded performance checks; retain report/run IDs and
  dataset manifest.

**Приёмка:** all required checks pass; deterministic demo totals are documented
  and independently recomputable; no test uses production PII or credentials;
  release evidence identifies exact schema/algorithm/policy versions.

## Порядок поставки

1. **Foundation:** 167.1–167.5 — policy, channel identity, contracts, source
   facts and historical COGS.
2. **Calculation correctness:** 167.6–167.11 — allocations, reconciliation,
   ads/returns, FX and immutable calculation runs.
3. **Runtime/reporting:** 167.12–167.15 — ClickHouse, PostgreSQL metadata,
   worker, API and exports.
4. **Operator surface:** 167.16–167.17 — UI, security, observability and
   quotas.
5. **Release:** 167.18 — full qualification, Compose, docs and evidence.

## Явно исключено из Task 167

- бухгалтерский GL, налоговая декларация, statutory P&L и закрытие периода;
- изменение Order/OrderItem/Payment/Inventory/SettlementEntry задним числом;
- автоматическое изменение цен, рекламы, поставок, refunds или channel mapping
  из отчёта;
- provider-specific ветки в Core, browser scraping и неподтверждённые API;
- смешивание `order_accrual`, `settlement` и `cash` в одной цифре;
- сложение валют без Task-089b evidence, zero-fill missing facts и silent
  rounding;
- AI/LLM как источник финансовой истины или автоматический attribution;
- произвольная формула из UI, raw SQL/HTTP/код в пользовательском payload;
- считать SDK-only/health-only коннектор полноценным каналом продаж;
- использование ClickHouse, Kafka, Valkey или browser state как transactional
  source of truth.

## Gate RUNTIME-167

- каждая строка имеет tenant-scoped `channel_ref`, basis, currency, formula /
  allocation / valuation / attribution versions и immutable input digest;
- показатели и знаки согласованы с утверждённой policy, а суммы сохраняют
  conservation относительно Order/Settlement/Payment/Ads/COGS facts;
- `complete`, `partial`, `stale`, `unmatched`, `conflict`, `mixed_currency` и
  `unsupported` различаются; отсутствие факта никогда не становится нулём;
- payout, settlement sale, Order revenue и Payment fee не создают двойного
  счёта; returns/refunds/cancellations и adjustment chain воспроизводимы;
- cross-currency totals используют только immutable Task-089b conversion
  evidence; missing/stale FX fail closed;
- published calculation snapshots immutable, replay/backfill/idempotency-safe;
  ClickHouse rebuild не меняет authoritative facts;
- API/OpenAPI/SDK/UI показывают источник, freshness, coverage, quality и
  explanation, а exports воспроизводят выбранный run;
- RLS, permissions, privacy, quotas, no-secrets logging, audit/lineage и
  retention/legal-hold checks проходят;
- runtime worker выдерживает crash, lease loss, duplicate/out-of-order events,
  late settlements, CH outage and DLQ replay without false profit or retry storm;
- только synthetic Compose/live evidence и deterministic test fixtures могут
  подтвердить release; all Go, contract, architecture, migration, frontend,
  conformance, performance and documentation checks PASS before production.

## Связанные материалы

## Фактически поставлено

- `internal/core/uniteconomics` — deterministic aggregation, exact money,
  basis validation, settlement/provider-reference deduplication, payout
  separation, fixed-point margin, overflow/mixed-currency fail-closed and
  explicit component quality states.
- `000034_channel_unit_economics.sql` — channel dimension, order attribution,
  historical COGS snapshots, immutable calculation-run metadata and quality
  issues with forced tenant RLS; settlement kinds are extended additively.
- `GET /api/v1/reports/unit_economics_by_channel` — bounded channel/currency/
  basis filters and CSV/PDF-compatible report rows sourced from PostgreSQL;
  ClickHouse fallback never becomes a financial source of truth.
- Reports UI — Russian labels, basis/channel controls, exact minor-unit
  formatting, quality/coverage visibility and explicit incomplete-data note.
- OpenAPI, generated SDKs, event schemas, fixtures, ADR, architecture review,
  migration catalog and operations documentation are synchronized.

Release evidence must still include a Compose run with synthetic tenant data,
source watermarks and a retained report/run digest before production admission.

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/02-domain-model.md`
- `docs/03-module-boundaries.md`
- `docs/05-database.md`
- `docs/06-api.md`
- `docs/08-sync-reconciliation.md`
- `docs/12-reporting-bi.md`
- `docs/29-data-lineage.md`
- `docs/37-growth-advertising-promotions.md`
- `docs/42-marketplace-settlements-ledger.md`
- `docs/46-sre-performance-slo.md`
- `docs/52-data-retention-archival.md`
- `docs/55-legal-entity-counterparty-core.md`
- `docs/56-product-compliance.md`
- `docs/63-fx-exchange-rate-provider.md`
- `adr/0004-clickhouse-analytics.md`
- `adr/0014-settlement-ledger.md`
- `adr/0031-fx-rate-provider.md`
- `adr/0052-clickhouse-reporting-projections.md`
- `adr/0054-advertising-engine.md`
- `adr/0061-marketplace-settlement-ledger.md`
- `adr/0062-settlement-payment-reconciliation.md`
- `tasks/issues/006-orders.md`
- `tasks/issues/049-clickhouse-reporting-foundation.md`
- `tasks/issues/050-advertising-engine.md`
- `tasks/issues/058-marketplace-settlement-ledger.md`
- `tasks/issues/059-settlement-payment-reconciliation.md`
- `tasks/issues/061-retention-subject-requests-and-tenant-deletion.md`
- `tasks/issues/089-fx-rate-provider.md`
- `tasks/issues/164-returns-cancellations-refunds.md`
