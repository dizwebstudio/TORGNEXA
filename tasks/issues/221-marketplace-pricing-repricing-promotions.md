# Task 221 — Массовые цены, repricing, Buy Box и продвижение

## Status

`planned` — существующие pricing guards, unit economics и read-only рекламная
аналитика дают основу, но production-ready массовое принятие и применение
ценовых решений ещё не реализовано.

## Objective

Довести provider-neutral контур от наблюдения рынка до безопасного изменения
цены, участия в акции и управления рекламной кампанией. Оператор должен видеть
не только текущую цену и прибыль, но и объяснение решения: цена конкурентов,
позиция в Buy Box, комиссия, логистика, себестоимость, рекламные расходы,
минимальная маржа и причина, по которой изменение разрешено или заблокировано.

Контур должен поддерживать массовый preview, правила repricing, минимальную
цену, защиту маржи, ограничения по бюджету/количеству SKU, approval для
опасных изменений, идемпотентное применение, read-after-write и reconciliation.
Провайдеры, которые не дают официальные данные о конкурентах или Buy Box,
должны честно возвращать `not_available`/`unknown`; эти значения нельзя
подменять расчётом «самая низкая цена = Buy Box».

## Target end-to-end slice

Для одного канала и набора Offer/SKU должен проходить сценарий:

1. загрузить текущие цены, комиссии, остатки, продажи, рекламные и promotion
   facts с quality/freshness;
2. загрузить официальные market observations, если capability доступна:
   цены конкурентов, позицию, Buy Box и время наблюдения;
3. рассчитать effective price, net revenue, contribution margin, floor price и
   допустимый диапазон изменения в точной валюте;
4. прогнать bounded rule engine и получить preview с каждой строкой,
   объяснением, изменением в рублях/процентах, маржой до/после и блокерами;
5. запросить approval, если превышены blast radius, процент изменения,
   бюджет, риск маржи или цена акции;
6. применить только разрешённые строки через typed connector capability,
   сохранить idempotency receipt и проверить фактическую удалённую цену;
7. при timeout, conflict, rate limit или неизвестном remote result перевести
   строку в `unknown`/`manual_attention`, не повторяя запись вслепую;
8. показать оператору результат, Buy Box/position delta, отклонённые строки,
   audit trail и безопасное действие для повторной сверки.

### Definition of done

На synthetic WB/Ozon/Yandex Market fixtures проходит пакет минимум из 1 000
SKU с dry-run и ограниченным live-like применением. Ни одна строка не может
уйти ниже floor price, minimum margin, явно заданной минимальной цены или
лимита бюджета. Повтор команды, события, webhook и worker lease не создаёт
второе изменение цены, участие в акции или рекламное списание.

## Architecture boundaries

- Core оперирует `PricingRule`, `PriceCandidate`, `MarketObservation`,
  `BuyBoxObservation`, `PromotionParticipation`, `AdvertisingOperation` и
  `PriceChangeReceipt`, но не знает названий маркетплейсов и их JSON-полей.
- Внешние market observations и Buy Box facts приходят только через typed
  connector ports. Browser scraping, неофициальные кабинеты и обход лимитов
  запрещены; отсутствие официального API — это `not_available`.
- `Offer` и история цен не переписываются. Каждое применение создаёт новую
  версию/receipt с причиной, rule version, input snapshot digest и remote ID.
- Цена считается integer minor units + ISO currency; проценты и коэффициенты —
  basis points/fixed-point. Float и cross-currency сравнение запрещены.
- Unit economics остаётся источником фактических себестоимости, комиссий,
  логистики, рекламы и качества. Repricing не может обнулять missing facts и
  считать неизвестные расходы равными нулю.
- Массовая запись является `write_sensitive`; акции, ставки, бюджеты и
  снижение ниже policy threshold требуют approval. Dry-run не вызывает remote
  write и не создаёт внешний side effect.
- Worker и webhooks используют Transactional Outbox/Inbox, tenant scope,
  idempotency, lease fencing, rate-limit budgets и reconciliation. Секреты и
  raw provider payloads не попадают в PostgreSQL facts, события, логи или UI.

## Subtasks and implementation order

### 221.1 — ADR, terminology and pricing policy matrix

**Depends on:** none.

- Зафиксировать ADR и словарь: list price, effective price, floor price,
  minimum margin, landed cost, Buy Box, price position, market observation,
  repricing run, promotion participation, bid, budget, blast radius.
- Утвердить порядок расчёта: налог, комиссия, доставка, хранение, реклама,
  скидка продавца, субсидия маркетплейса, валюта и округление.
- Описать policy для изменения цены, акции, ставки, бюджета и массового
  включения товара; определить auto-safe, approval-required и blocked cases.
- Задать freshness TTL для market observation, unit economics, FX, inventory и
  advertising facts; устаревшие данные не дают права на live write.
- Утвердить Buy Box semantics: provider-observed win/loss, position, reason,
  `not_available`, `unknown`; отдельным полем хранить confidence/quality.

**Acceptance:** для каждого решения есть формула, источник, срок годности,
валюта, owner policy и требуемое evidence; нет скрытого правила «самая низкая
цена всегда выигрывает».

### 221.2 — Domain contracts and deterministic calculation engine

**Depends on:** 221.1.

- Ввести typed contracts для market observations, competitor offers, Buy Box,
  price candidates, repricing rules, guard violations and explanations.
- Реализовать точные вычисления effective price, net proceeds, margin,
  contribution profit, floor price и допустимого изменения.
- Поддержать fixed rules: match/undercut competitor, target position, target
  margin, stock/velocity, schedule, channel/warehouse scope and max step.
- Добавить hysteresis/cooldown/deadband, чтобы цена не oscillated при равных
  наблюдениях и не менялась чаще установленного окна.
- Сортировка и результат расчёта должны быть детерминированными; одинаковый
  input snapshot и rule version дают одинаковый preview digest.

**Acceptance:** tests покрывают округление, tax/fee/logistics, missing facts,
negative margin, floor/max price, max percentage step, duplicate SKU,
cross-currency, stale observation, cooldown и одинаковый preview digest.

### 221.3 — Market observations and Buy Box read runtime

**Depends on:** 221.1–221.2.

- Добавить provider-neutral `MarketObservationReader` с bounded query,
  cursor/limit, observation time, source quality и remote identifiers.
- Нормализовать competitor price, availability, delivery promise, seller/rank,
  Buy Box state/position и provider reason только если они официально доступны.
- Ввести daily/intraday worker с watermark, deduplication и retention; delayed,
  partial и contradictory observations должны быть видимы в reconciliation.
- Для провайдера без Buy Box/competitor API отображать capability gap и не
  включать соответствующий rule в live mode.
- Не сохранять customer PII, raw response, cookies, токены и не использовать
  scraping.

**Acceptance:** fixtures на available/not_available/unknown, stale data,
pagination, duplicate observations, out-of-order observation и provider
rate-limit; UI и API различают отсутствие функции и ошибку загрузки.

### 221.4 — Repricing rule engine and preview runs

**Depends on:** 221.2–221.3.

- Создать immutable versioned rules with scope: workspace, account, channel,
  category, product, offer/SKU; более узкое правило должно иметь явный
  priority/precedence.
- Поддержать dry-run run lifecycle `queued → running → completed|partial|failed`
  с input digest, rule digest, counters and per-SKU explanation.
- Сформировать candidate actions без remote side effect: current/proposed price,
  floor, expected margin, competitor delta, Buy Box evidence, violations and
  approval requirement.
- Добавить batch partitioning, maximum affected SKUs, maximum total price delta,
  per-run and per-day limits.
- Для конфликтующих правил возвращать deterministic conflict, а не выбирать
  последнее записанное правило.

**Acceptance:** preview на 1 000 SKU укладывается в bounded limits, повторный
  preview даёт тот же digest, blocked rows не попадают в apply batch, а каждая
  разрешённая строка имеет объяснение на русском языке.

### 221.5 — PostgreSQL persistence, RLS, lineage and retention

**Depends on:** 221.2–221.4.

- Добавить expand-only migration для pricing rules/versions, floor/margin
  policies, observations, Buy Box facts, repricing runs/candidates, receipts,
  explanations and reconciliation findings.
- Включить `FORCE ROW LEVEL SECURITY`, tenant composite predicates,
  optimistic versions, idempotency uniqueness and bounded indexes.
- Сохранять snapshot digests и typed source references, но не raw provider data.
- Установить retention для high-frequency observations отдельно от audit,
  financial evidence и active rule history; legal hold имеет приоритет.
- Запретить delete/update append-only receipts, observations used in a live
  decision and audit evidence.

**Acceptance:** migration static checks, fresh/upgrade rehearsal, two-tenant RLS
smoke, cross-tenant negative tests, uniqueness/append-only tests and bounded
query plans pass.

### 221.6 — Safe price-write orchestration

**Depends on:** 221.1–221.5.

- Создать typed `PriceWriteOperation` с current version, proposed amount,
  account/capability, rule snapshot, approval reference and idempotency key.
- Перед remote call перечитать current Offer/price, stock, cost, fees,
  observation freshness, policy, approval, account health and connector
  capability; stale preview must be rejected.
- Обеспечить dry-run, deterministic remote idempotency identity, rate-limit
  budget, retry only for safe transient errors and read-after-write.
- Normalize `accepted`, `applied`, `rejected`, `conflict`, `unknown` and
  `manual_attention`; unknown must go to reconciliation without blind retry.
- Publish price-changed event only after local receipt is durable and preserve
  previous price as immutable history.

**Acceptance:** tests cover duplicate apply, concurrent edit/version conflict,
approval expiry, floor violation after preview, timeout after remote accept,
rate-limit, webhook-before-response, worker crash and read-after-write mismatch.

### 221.7 — Promotions and discount participation

**Depends on:** 221.1–221.6.

- Добавить чтение календаря акций, eligibility, discount/subsidy, start/end,
  stock limits and marketplace constraints through `promotions.read`.
- Создать promotion preview with effective seller price, subsidy allocation,
  fee impact, expected margin, inventory risk and conflicting promotions.
- Реализовать approval-bound participation/withdraw/update only through
  `promotions.manage`, with idempotency, dry-run where supported and
  read-after-write.
- Не допускать overlapping promotions, price below floor, negative margin,
  unbounded SKU count or change after promotion deadline.
- Link promotion facts to unit economics and distinguish seller discount from
  marketplace subsidy; no double counting.

**Acceptance:** full/partial campaign participation, withdrawal, schedule edge,
duplicate request, provider rejects, missing fee/subsidy, over-allocation and
unknown remote outcome are covered by deterministic tests and reconciliation.

### 221.8 — Advertising campaign, bid and budget management

**Depends on:** 221.1, 221.4–221.7.

- Расширить текущий read-only advertising MVP до approval-bound `ads.manage`
  для typed operations: launch, stop, pause, set budget, set bid, link products,
  archive.
- Ввести campaign/bid/budget policy: daily spend ceiling, total spend ceiling,
  max bid, minimum ROAS/ROMI/ДРР, max affected products and cooldown.
- Preview должен показывать expected spend, attribution quality, margin impact,
  current/proposed bid and budget, plus missing-data blockers.
- Separate campaign lifecycle from pricing/promotion; price changes cannot
  silently change a live advertising budget.
- Normalize provider results and reconcile spend/budget after timeout or webhook;
  no duplicate budget increase.

**Acceptance:** synthetic launch/pause/bid/budget/link operations pass approval,
quota, idempotency, unknown result and reconciliation tests; no ads write is
enabled from manifest alone.

### 221.9 — Connector capability qualification

**Depends on:** 221.3, 221.6–221.8.

- Создать matrix по каждому connector account для `prices.read`/`prices.write`,
  `market.observations.read`, `buy_box.read`, `promotions.read`/
  `promotions.manage`, `ads.read`/`ads.manage`.
- Разделить SDK contract, runtime route, provider fixture и live/Docker
  qualification; `manifest.json` сам по себе не допускает capability.
- Для WB/Ozon/Yandex Market сначала квалифицировать наиболее ценный набор:
  price read/write, competitor/Buy Box read if official, promotion read/manage,
  ads manage and reconciliation.
- Keep unsupported capability visibly `not_available`/`qualification_required`;
  UI, workflow, MCP and API must all enforce the same guard.
- Document provider-specific rate limits, field quality, action semantics and
  remote read-after-write behavior outside Core.

**Acceptance:** generated capability catalog equals runtime support; every
`enabled` write has conformance evidence, and unsupported operations are
negative-tested through API, worker, UI and automation boundaries.

### 221.10 — API, OpenAPI, generated SDK and MCP boundary

**Depends on:** 221.4–221.9.

- Добавить cursor-based routes for rule versions, observation feed, Buy Box,
  preview/run details, candidate approval, apply/retry/manual resolution and
  reconciliation.
- Add promotion calendar/preview/participation and typed advertising operation
  routes; every mutation requires Idempotency-Key and expected version.
- Update OpenAPI, TypeScript/Go SDK, capability contract, event catalog and
  permission labels; expose Russian labels while preserving stable codes.
- MCP/OpenClaw may request a price-change preview but cannot directly bypass
  approval, money/quantity limits or host-side policy.
- Errors must identify stale data, blocked floor/margin, unavailable capability,
  conflict, rate-limit and unknown outcome separately.

**Acceptance:** contract tests prove schema parity, tenant isolation, cursor
pagination, idempotent replay, safe error mapping and no raw provider payload.

### 221.11 — Pricing Center operator UI

**Depends on:** 221.4, 221.6–221.10.

- Добавить раздел «Цены и продвижение» с вкладками «Правила», «Массовый
  preview», «Конкуренты», «Buy Box», «Акции», «Реклама» и «История запусков».
- В preview показывать current/proposed/effective price, floor, margin, fees,
  competitor delta, Buy Box evidence, stock risk, freshness and block reason.
- Массовое применение — двухшаговое: preview → выбор разрешённых строк →
  approval/confirm → progress/result; blocked/unknown строки нельзя отметить
  как успешно применённые.
- Добавить фильтры по каналу, кабинету, SKU, правилу, статусу и качеству
  данных; состояния `not_available`, `unknown`, `stale` и `manual_attention`
  должны быть русифицированы и различимы.
- Все операции имеют retry-safe кнопку, audit timeline, export preview и
  понятное объяснение «почему цена изменилась».

**Acceptance:** keyboard navigation/focus, responsive table, 1 000-row preview,
error/retry states, stale data warning, approval flow and no-success-on-unknown
browser tests pass.

### 221.12 — Security, quotas, observability and reconciliation

**Depends on:** 221.5–221.11.

- Метрики: preview/apply latency, candidate count, blocked floor/margin rows,
  price delta, Buy Box freshness, observation lag, approval wait, unknown rate,
  provider rejection, rate-limit, spend/budget and reconciliation lag.
- Add per-workspace quotas for active rules, observations, affected SKUs,
  price delta/day, bid/budget spend, remote calls and concurrent runs.
- Audit actor, policy/rule version, before/after price, input digest, approval,
  remote receipt and result; no secrets, raw payloads or unnecessary PII.
- Add kill switch per workspace/account/capability for price, promotion and ads
  writes; it must stop new side effects without hiding existing evidence.
- Run reconciliation for local/remote prices, promotion participation, Buy Box
  observations and advertising budgets; unknown never becomes succeeded by age.

**Acceptance:** quota isolation, kill-switch, incident runbook, alert thresholds,
manual resolution and cross-tenant/security negative tests pass.

### 221.13 — Compose E2E, load, conformance and documentation

**Depends on:** all previous subtasks.

- Build synthetic marketplace fixtures with competitor prices, Buy Box states,
  promotion calendar, fees, rate limits, rejected writes and ambiguous outcomes.
- Cover full cycle for one SKU and 1 000 SKU batch: observe → calculate →
  preview → approve → write → read-after-write → reconcile.
- Cover price oscillation, stale facts, partial apply, provider outage, duplicate
  webhook, crash after remote accept, lease loss, concurrent rule update,
  promotion overlap and budget breach.
- Run load profile for scheduled repricing, observation ingestion and webhook
  bursts on the small Compose topology; verify bounded DB/Kafka/worker usage.
- Update operations runbooks, API/events docs, connector qualification matrix,
  UI screenshots, demo data and release checklist.

**Acceptance:** `go test ./...`, `go vet ./...`, contract/architecture/migration
checks, frontend typecheck/build, connector conformance and Compose E2E pass;
release is blocked until at least one marketplace has current write evidence for
prices and one for promotions or ads management.

## Suggested delivery slices

1. **Pricing safety core:** 221.1–221.5 — policy, calculations, observations,
   preview and durable evidence.
2. **One marketplace vertical slice:** 221.6 + 221.9 for one fully qualified
   provider, including price write, read-after-write and reconciliation.
3. **Growth controls:** 221.7–221.8 — promotions, bids, budgets and approval.
4. **Operator product:** 221.10–221.12 — API, SDK, UI, quotas and operations.
5. **Production gate:** 221.13 — fixtures, Compose, load, conformance and docs.

## Explicit exclusions

- scraping competitor prices, browser automation and undocumented provider APIs;
- provider-specific branches in Core or a generic unbounded rule language;
- automatic price changes when cost, fee, FX, stock, observation or approval data
  is stale/unknown;
- using lowest observed competitor price as a guaranteed Buy Box result;
- silent changes to financial facts, settlement ledger, historic price evidence
  or unit economics snapshots;
- automatic ad/promotion spend without budget ceiling, idempotency, approval
  policy and reconciliation path;
- claiming a connector capability from SDK/manifest presence without runtime and
  current qualification evidence.

## References

- `docs/37-growth-advertising-promotions.md`
- `docs/operations/051-promotions-pricing-guards.md`
- `docs/operations/050-advertising-engine.md`
- `docs/operations/220-marketplace-advertising-runtime.md`
- `tasks/issues/167-channel-unit-economics.md`
- `tasks/issues/217-marketplace-product-publication.md`
- `tasks/issues/220-marketplace-advertising-runtime.md`
- `adr/0053-promotions-pricing-guards.md`
- `adr/0054-advertising-engine.md`
- `adr/0172-marketplace-advertising-runtime.md`
