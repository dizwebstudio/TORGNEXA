# Task 225 — Акции, скидки, рекламные ставки и бюджеты

## Статус

`planned` — read-only факты рекламы и аналитика уже доступны, а pricing guards
и preview защищают минимальную цену/маржу. Управление акциями, ставками,
бюджетами и товарами кампаний пока не допущено до внешней записи.

## Цель

Закрыть provider-neutral контур от планирования до безопасного применения:

```text
акция/кампания → preview → quality и floor-price gate → approval
→ запуск/изменение ставки или бюджета → read-after-write → сверка расходов
```

Оператор должен видеть итоговую цену для покупателя и продавца, скидку,
субсидию площадки, комиссию, advertising spend, ставку, дневной/общий бюджет,
маржу и причину каждого разрешения или блокировки. Все внешние изменения
остаются capability-based и не должны создавать второй pricing, P&L или
financial ledger.

## Что уже есть и что закрывает этот task

- Task 050 содержит provider-neutral модели Campaign/AdGroup/Bid/Budget/Creative,
  лимиты, dry-run и approval thresholds.
- Task 051 содержит Promotion/Coupon/Discount и floor-price/minimum-margin
  guards для preview.
- Task 220 даёт read-only синхронизацию кампаний, расходов, performance,
  метрик и reconciliation для WB/Ozon.
- Task 221 даёт pricing preview, repricing guards и общий контур
  qualification для promotion/advertising writes.

Task 225 соединяет эти части с реальной записью и массовым управлением.
Состояния `not_available`, `qualification_required`, `unknown` и
`manual_attention` должны отображаться честно; наличие SDK или capability в
manifest не считается работающей операцией.

## Сквозной сценарий

Для одной акции и рекламной кампании, связанных с набором Offer/SKU, должны
поддерживаться:

1. загрузка официальных ограничений канала, календаря акции, eligibility,
   комиссий, субсидий, текущих ставок и бюджетов;
2. расчёт effective seller/consumer price, floor price, minimum margin,
   expected spend, ROAS/ROMI/ДРР и stock risk;
3. preview с построчным diff, affected SKU, конфликтами, лимитами и
   объяснением на русском языке;
4. approval для запуска, изменения бюджета/ставки, массового изменения товаров
   и операций с высоким финансовым blast radius;
5. typed remote apply с idempotency, dry-run если он поддержан, rate-limit
   budget и read-after-write;
6. обработка partial/rejected/unknown результата без слепого повтора;
7. reconciliation локальной кампании, участия в акции, remote price, spend,
   budget и финансовых фактов.

## Подзадачи

### 225.1 — ADR и policy matrix для продвижения

**Зависимости:** 050, 051, 220, 221.

- Зафиксировать словарь и границы: акция, coupon, seller discount, marketplace
  subsidy, effective price, campaign, ad group, bid, budget, pacing, spend,
  eligibility и attribution.
- Определить владельцев данных: PIM/Offer, pricing, promotion, advertising,
  unit economics, inventory и settlement.
- Разделить auto-safe, approval-required, blocked и unsupported операции.
- Утвердить TTL для цены, stock, unit economics, FX, eligibility, spend и
  remote campaign state.
- Описать политику остановки при negative margin, floor violation, stock risk,
  budget exhaustion, stale data и неизвестном внешнем результате.

**Acceptance:** для каждой операции определены формула, источник, валюта,
лимит, approval policy, rollback/reconciliation action и owner; marketplace
не может незаметно изменить каноническую цену или финансовый ledger.

### 225.2 — Каноническая модель акций, скидок и eligibility

**Зависимости:** 225.1.

- Расширить typed Promotion/Coupon/Discount модель: период, канал, аккаунт,
  SKU, категория, условия, лимит участия, seller discount и subsidy.
- Поддержать чтение календаря акций, eligibility, минимальной цены, правил
  совмещения и ограничений площадки.
- Различать цену продавца, цену для покупателя, скидку продавца и субсидию
  marketplace; не считать субсидию дополнительной выручкой без evidence.
- Хранить source/version/freshness правил, remote mapping и reason отказа.
- Ввести состояния `draft`, `scheduled`, `active`, `paused`, `completed`,
  `cancelled`, `rejected`, `unknown` с допустимыми переходами.

**Acceptance:** модель поддерживает полную и частичную eligibility, повторную
синхронизацию и конфликтующие акции; outdated rules не могут попасть в live
apply, а удаление участия сохраняет историю.

### 225.3 — Правила скидок и экономический расчёт

**Зависимости:** 225.1–225.2, 167, 221.

- Рассчитывать effective price, net proceeds, commission, logistics,
  advertising, promotion cost, contribution profit и margin в integer minor
  units/fixed-point basis points.
- Применять floor price, minimum margin, maximum discount, max price delta,
  seller budget и channel restrictions до любых массовых изменений.
- Учитывать приоритет скидок, несовместимые акции, stacking, округление и
  разные валюты без float/cross-currency сравнения.
- Missing/stale cost, fee, FX, spend или subsidy facts должны блокировать либо
  явно понижать качество решения, но не превращаться в ноль.
- Связать изменение промо с unit economics и P&L без двойного учёта скидок,
  рекламы и субсидий.

**Acceptance:** тесты покрывают скидку ниже floor, negative margin, stacking,
округление, missing facts, stale facts, лимит бюджета, валютный конфликт и
детерминированный результат при одинаковом snapshot.

### 225.4 — Preview и массовое участие в акции

**Зависимости:** 225.2–225.3.

- Добавить выбор товаров по каналу, кабинету, категории, SKU, статусу,
  остатку, марже и eligibility с tenant/workspace scope.
- Формировать bounded preview с affected count, before/after price, discount,
  subsidy, margin, floor, stock risk, conflicts и blocked rows.
- Поддержать dry-run, immutable input snapshot, rule version, stable digest,
  per-row explanation и partitioning массовой операции.
- Массовое вступление, изменение и выход из акции требуют явного набора SKU,
  quota, approval и idempotency; clear/delete не допускаются по умолчанию.
- Частичный ответ хранить построчно: `applied`, `rejected`, `unknown`,
  `manual_attention`, а не скрывать его под общим success.

**Acceptance:** preview на 1 000 SKU ограничен и воспроизводим; невалидные
строки не уходят во внешний apply, повтор не создаёт второе участие, diff
доступен после завершения и не пересекает tenant.

### 225.5 — Жизненный цикл рекламных кампаний

**Зависимости:** 050, 220, 225.1.

- Описать typed операции `create`, `launch`, `pause`, `resume`, `stop`,
  `archive`, `link_products` и `unlink_products` только для поддержанных
  connector capabilities.
- Связать Campaign/AdGroup/Creative с Offer/SKU, но не менять Product truth;
  проверять допустимость товара, контента, категории и статуса публикации.
- Поддержать schedule, timezone, campaign objective, attribution source и
  ограничения по количеству товаров.
- Отделить lifecycle кампании от цены и акции: остановка рекламы не должна
  молча менять цену, а изменение цены не должно менять рекламный бюджет.
- Сохранять immutable intent, approval, remote receipt и локальное состояние.

**Acceptance:** create/update/link/pause/stop/archive проходят state-machine и
permission checks; повтор операции идемпотентен; недоступная capability
отказывается до remote call.

### 225.6 — Ставки, стратегии и бюджеты

**Зависимости:** 225.3, 225.5.

- Поддержать provider-neutral bid strategy и typed bid units только в рамках
  официально поддержанных моделей CPC/CPM/других единиц; неизвестную модель не
  преобразовывать приблизительно.
- Ввести daily/lifetime budget, spend ceiling, max bid, pacing, cooldown,
  minimum ROAS/ROMI/ДРР, minimum margin и max affected campaigns/SKU.
- Рассчитывать preview proposed bid/budget, expected spend, margin impact,
  stock risk и missing-data blockers.
- Запретить бюджет ниже уже подтверждённого spend, отрицательные значения,
  валютный конфликт и повышение выше policy cap.
- Для автоматической стратегии хранить versioned rule, input snapshot и
  возможность pause/kill switch.

**Acceptance:** тесты покрывают bid/budget floor/ceiling, daily/total cap,
overspend prevention, currency, cooldown, strategy mismatch, concurrent update,
unknown spend и approval threshold.

### 225.7 — Approval-bound apply и durable orchestration

**Зависимости:** 225.4–225.6.

- Создать общий typed operation для promotion/ad writes с current version,
  preview digest, approval reference, capability и Idempotency-Key.
- Перед записью заново проверить current price, floor/margin, eligibility,
  stock, budget, account health, policy, quota и freshness.
- Реализовать Transactional Outbox/Inbox, worker lease/fencing, safe retries,
  backoff/jitter, deadlines и recovery после crash.
- Нормализовать `accepted`, `applied`, `rejected`, `conflict`, `rate_limited`,
  `unknown`, `manual_attention`; timeout after remote commit не повторять
  вслепую.
- Публиковать локальное событие только после durable receipt; хранить history
  предыдущего бюджета/ставки/участия.

**Acceptance:** duplicate apply, crash до/после remote call, webhook-before-
response, lease loss, approval expiry, stale preview, rate limit и read-after-
write mismatch не создают двойного запуска, списания или изменения бюджета.

### 225.8 — Connector ports и qualification

**Зависимости:** 225.2, 225.5–225.7.

- Разделить capabilities: `promotions.read`, `promotions.manage`, `ads.read`,
  `ads.manage`, а при необходимости — отдельные typed operations для bid,
  budget, product linking и creative.
- Для каждого connector проверить official API semantics, auth/SecretProvider,
  rate limits, dry-run, partial update, remote idempotency, webhook signature,
  read-after-write и unknown outcome.
- Сначала квалифицировать один marketplace для акций и один для рекламы;
  WB/Ozon/Yandex Market подключать только при наличии актуального evidence.
- Сохранить provider mapping внутри adapter; Core не должен ветвиться по имени
  marketplace и не должен использовать scraping.
- Синхронизировать capability catalog, runtime guard, UI, worker и MCP.

**Acceptance:** conformance matrix показывает `enabled`, `read_only`,
`qualification_required` или `not_available` для каждой операции; manifest/SDK
без runtime evidence не включает запись.

### 225.9 — Persistence, API, OpenAPI, SDK и MCP

**Зависимости:** 225.2–225.8.

- Добавить expand-only хранение promotion rules/participation, campaign actions,
  bids, budgets, preview runs, approvals, receipts, spend snapshots и drift.
- Включить `FORCE ROW LEVEL SECURITY`, optimistic version, idempotency
  uniqueness, bounded indexes и append-only audit/financial evidence.
- Добавить cursor API для календаря, eligibility, preview, apply, history,
  budgets, bids, operation status и reconciliation.
- Обновить OpenAPI, Go/TypeScript/Python SDK, events, permissions и русские
  подписи, сохранив стабильные технические codes.
- MCP/OpenClaw разрешить preview и чтение, но запретить самостоятельное
  approval/apply, обход quota, floor/margin и budget policy.

**Acceptance:** contract parity, migration checks, two-tenant RLS, negative
permission tests, safe errors, cursor pagination and SDK drift checks pass;
raw provider payloads, secrets and customer PII не сохраняются.

### 225.10 — UI «Акции и реклама»

**Зависимости:** 225.4, 225.5–225.9.

- Расширить раздел «Реклама» вкладками «Кампании», «Акции», «Ставки и
  бюджеты», «Массовые операции», «Расходы» и «Сверка».
- В каждой строке показывать current/proposed value, effective price,
  discount/subsidy, floor, margin, spend, budget, freshness, channel capability
  и объяснение блокировки.
- Сделать двухшаговое массовое действие: preview → выбор разрешённых строк →
  approval/confirm → progress/result; blocked/unknown нельзя отметить success.
- Показать состояния `Нет данных`, `Не поддерживается каналом`, `Нужна
  квалификация`, `Неизвестный результат`, `Требует внимания` и `Ошибка` как
  разные русские состояния.
- Добавить безопасные retry/reconcile/pause, audit timeline, export preview,
  адаптивную таблицу и keyboard/focus support.

**Acceptance:** browser tests покрывают фильтры, preview 1 000 SKU, approval,
  bid/budget edit, pause, partial result, unknown, retry, kill switch и
  отсутствие success при неподтверждённой внешней операции.

### 225.11 — Reconciliation, наблюдаемость и контроль расходов

**Зависимости:** 225.7–225.10.

- Сверять local ↔ remote по campaign state, promotion participation, price,
  bid, daily/total budget, spend, linked products и attribution.
- Добавить findings для remote accepted/local timeout, stale calendar,
  overspend, budget mismatch, unknown apply, missing subsidy и delayed facts.
- Метрики: apply latency, rejection/unknown rate, spend lag, budget utilization,
  floor/margin blocks, approval wait, duplicate suppression, connector errors и
  reconciliation age.
- Ввести per-workspace/account quotas, daily spend caps, concurrency limits и
  kill switch для promotion/ads writes.
- Подготовить runbooks для зависшей кампании, ошибочной ставки, перерасхода,
  отмены акции, неизвестного remote result и восстановления после outage.

**Acceptance:** искусственное расхождение появляется в очереди с owner/SLA;
  остановка write не скрывает evidence; разрешение finding не требует ручного
  редактирования финансового ledger.

### 225.12 — Demo, E2E, load и release gate

**Зависимости:** все предыдущие подзадачи.

- Добавить synthetic campaign/promotion fixtures с active/paused/rejected,
  eligibility, seller discount, subsidy, competitor conflict, budgets,
  bids, spend и provider rate limits.
- Пройти сценарий: выбрать 1 000 SKU → preview акции → approval → apply →
  read-after-write → изменить ставку/бюджет → pause → reconcile.
- Отдельно проверить floor/margin block, overlapping promotions, budget breach,
  duplicate command, crash after remote accept, out-of-order webhook,
  concurrent edit, missing facts, unknown result, cross-tenant и permission
  denial.
- Добавить authenticated browser E2E и Compose API/worker E2E с synthetic
  marketplace connectors; live qualification хранить отдельно от repository
  PASS.
- Обновить документацию, demo data, connector matrix и release checklist;
  production write включать только при актуальном evidence.

**Acceptance:** `go test ./...`, `go vet ./...`, contract/architecture/migration
checks, frontend typecheck/build, connector conformance и Compose E2E проходят;
ни одна операция не создаёт двойное участие, запуск, списание, ставку или
бюджетное изменение.

## Не входит в этот task

- Новые алгоритмы repricing и Buy Box — Task 221.
- Read-only ingestion рекламных фактов и базовая аналитика — Task 220.
- Неофициальный scraping, обход rate limits и автоматическое принятие
  неподтверждённых данных конкурентов.
- Финансовые chargeback/dispute и отдельный acquiring ledger.
- Автоматическая генерация креативов с самостоятельной публикацией AI.

## Зависимости

050, 051, 167, 217, 220, 221.

## Definition of Done

- Все 12 подзадач имеют implementation, contracts/docs и success/failure/
  idempotency tests.
- Preview, approval, apply, pause, retry, unknown и reconciliation доступны
  через API и UI с одинаковыми capability/policy guards.
- Repository qualification и live connector qualification разделены; без
  evidence операция остаётся read-only или `qualification_required`.
- Аудит, quotas, kill switch, rollback/runbooks и financial impact описаны;
  в fixtures нет production PII и секретов.
