# Task 224 — Сквозная обработка заказа: от резерва до возврата денег

## Статус

`repository-complete` (2026-09-03) — provider-neutral сквозной контур,
  контракты, API/UI, timeline, synthetic qualification, миграционный gate и
  fail-closed production evidence gate собраны. Production/live readiness
  официальных marketplace, carrier и payment/fiscal accounts остаётся
  external release-gate до запуска на целевых non-production accounts.

## Цель

Закрыть один проверяемый сквозной путь:

```text
заказ → резерв → сборка → этикетка → отгрузка → возврат → проверка возврата
→ refund → сверка
```

Task координирует существующие контуры заказов, остатков, WMS, логистики и
возвратов. Он не создаёт второй Order, Inventory, Fulfillment, Payment или
финансовый ledger. Provider-specific поведение должно оставаться за
connector capability и mapping-слоем.

## Что уже есть и что закрывает этот task

- Task 006 даёт canonical Order/OrderItem и lifecycle заказа.
- Tasks 054, 117 и 170 дают ledger/reservation/allocation и durable WMS
  execution, включая pick/pack и handoff внутри WMS.
- Task 074 даёт provider-neutral rate/create shipment/label/track/cancel/return
  ports.
- Task 164 даёт доменные cancellation/return/refund агрегаты и их API.
- Task 223 даёт marketplace control-plane и synthetic orchestration.

Репозиторный результат фиксирует эти части как один tenant-scoped процесс:
с единым flow/idempotency lineage, корректными unknown/blocked состояниями,
детальной timeline и сверкой ссылок после частичного возврата.

## Repository completion evidence — 2026-09-01

- `marketplaceoperations.NewAtStage` запускает golden path с уже
  материализованного canonical заказа; `label` — отдельная стадия между
  `pick_pack` и `shipment`.
- `GET /api/v1/marketplace-operations/flows/{flow_id}` возвращает flow и
  redacted append-only timeline из command journal; OpenAPI и Go/TypeScript/
  Python SDK обновлены.
- WMS, logistics, returns/refund, settlement и reconciliation остаются
  владельцами своих агрегатов. Orchestration projection не хранит raw payloads,
  токены, штрихкоды или private signing material.
- Добавлены ADR-0174, эксплуатационная документация и
  `make order-fulfillment-qualification`; synthetic golden path проверяет
  canonical references, duplicate/idempotency и unknown outcome.
- Миграция `000051_marketplace_order_fulfillment.sql` расширяет stage vocabulary
  для `label`, требует backup и не изменяет исторические бизнес-факты.

Внешняя qualification не объявляется автоматически: без официальных
non-production credentials и retained evidence соответствующий connector
остаётся `read_only`, `partially_supported` или `qualification_required`.

## Состояние подзадач

| Подзадача | Результат |
|---|---|
| 224.1 | Закрыта: ownership matrix и переходы закреплены в ADR-0174 и `StageContract`. |
| 224.2 | Закрыта: canonical materialization проверяет mapping, exact money/quantity, tax и duplicate-safe input. |
| 224.3 | Закрыта: reservation/allocation выполняются существующим atomic WMS boundary с outbox lineage. |
| 224.4 | Закрыта: durable pick/pack, scan digest, lease/fencing и exceptions доступны в WMS. |
| 224.5 | Закрыта: typed logistics rate/label/shipment ports; `label` — отдельный checkpoint. |
| 224.6 | Закрыта: handoff, tracking, webhook ordering и unknown outcome используют logistics runtime. |
| 224.7 | Закрыта: Task 164 даёт partial return, receiving, inspection и disposition. |
| 224.8 | Закрыта: refund allocation, fiscal/settlement lineage и over-refund guards остаются canonical. |
| 224.9 | Закрыта: bounded `LifecycleRunner` и append-only command journal обеспечивают продолжение без дублей. |
| 224.10 | Закрыта: flow detail/timeline API, SDK и операторский Marketplace UI. |
| 224.11 | Repository gate закрыт; credentialed official connector gate остаётся открытым. |
| 224.12 | Закрыта: findings, reconciliation actions, unknown/attention и operator visibility. |
| 224.13 | Synthetic repository gate закрыт командой `make order-fulfillment-qualification`; полный release gate закрыт командой `make production-golden-path`, credentialed evidence остаётся внешним входом. |

## Подзадачи

### 224.1 — Контракт golden path и ownership matrix

- Зафиксировать в ADR владельца каждого перехода: Order, Reservation/
  Fulfillment, WMS, Logistics, Return, Payment, Fiscal/Settlement.
- Описать допустимые переходы, terminal states, отмену до и после отгрузки,
  частичное исполнение, частичный возврат и классы результата
  `succeeded`/`failed`/`unknown`/`needs_attention`.
- Утвердить единые `correlation_id`, `causation_id`, idempotency key и
  внутренние/внешние mapping IDs.

**Acceptance:** ADR и контракт показывают владельца и источник истины для
каждого шага; ни один connector не меняет состояние чужого домена напрямую;
для неизвестного результата определён безопасный ручной маршрут.

### 224.2 — Приём заказа и нормализация

- Связать импорт/создание заказа с canonical Order/OrderItem и immutable
  commercial snapshot.
- Добавить deduplication по tenant/workspace и remote mapping; повтор webhook,
  команды или worker delivery не создаёт второй заказ.
- Проверять валюту, деньги в minor units, decimal quantity, налоговые и
  скидочные snapshot-факты до запуска резерва.
- Зафиксировать поведение отменённого, неполного и неизвестного заказа.

**Acceptance:** один synthetic order можно принять из повторяющихся входящих
сообщений, получить тот же internal ID и безопасно передать в reservation;
невалидный или cross-tenant input не создаёт бизнес-эффекта.

### 224.3 — Резерв и fulfillment allocation

- Атомарно проверить ATP и создать reservation/allocation на каждую строку,
  warehouse и exact quantity.
- Поддержать split allocation только через явное правило; недостаток товара,
  неактивный склад и terminal order должны завершаться без частично записанной
  операции.
- Реализовать release/replace при отмене, failover и нехватке товара с
  сохранением lineage; не менять физический `on_hand` догадкой.
- Опубликовать inventory/allocation events через transactional outbox.

**Acceptance:** повтор команды не удваивает резерв, allocation или outbox;
конкурирующий резерв корректно получает version/availability conflict;
после успешного резерва WMS получает ровно одну исполняемую allocation.

### 224.4 — Сборка: pick → pack

- Создавать durable WMS tasks из allocation с привязкой к order item,
  warehouse, location и exact quantity.
- Закрыть claim/lease/fencing, сканирование, пересорт, недостачу, замену и
  частичную сборку без потери уже подтверждённых операций.
- Не считать заказ собранным до выполнения всех обязательных строк либо
  явного разрешённого partial-fulfillment решения.
- Зафиксировать pack facts: фактические количества, места, вес/габариты,
  упаковку и actor; секреты и raw barcode payload не сохранять.

**Acceptance:** happy path переводит все строки в `packed`; повтор scan/complete
идемпотентен; потеря lease, конфликт версии и shortage переводят задачу в
понятное исключение и не списывают товар дважды.

### 224.5 — Тариф, этикетка и создание shipment

- Получать нормализованный rate/SLA и выбрать службу по policy, стоимости,
  сроку и ограничениям заказа.
- Идемпотентно создать shipment через typed logistics capability и получить
  label/tracking; сохранить только безопасные метаданные и ссылку на
  quarantined/released upload artifact по правилам загрузок.
- Обработать отмену, истёкшую этикетку, повторное получение label, rate limit,
  timeout и `unknown` remote result без слепого повтора.
- Связать shipment с pack и order item, включая partial shipment.

**Acceptance:** один shipment и одна действующая этикетка создаются на один
идемпотентный intent; при неизвестном ответе оператор видит reconciliation
задачу, а не ложный `created`.

### 224.6 — Передача и отгрузка

- Оформить локальный handoff из WMS в shipment и typed carrier dispatch/
  manifest capability, если она поддерживается.
- Зафиксировать точку перехода `packed` → `shipped/dispatched`, tracking и
  внешний remote status; входящие webhook-и должны быть подписаны, проверены,
  deduplicated и устойчивы к out-of-order доставке.
- Обновлять marketplace status только после подтверждённого внешнего
  результата и capability check.
- Провести inventory ledger movement/reservation release в согласованный
  момент отгрузки, без silent rewrite `on_hand`.

**Acceptance:** повтор handoff/dispatch не создаёт вторую перевозку,
marketplace status или inventory movement; timeout/late webhook остаётся
`unknown`/`needs_attention` до reconciliation.

### 224.7 — Возврат товара и disposition

- Использовать Task 164 для authorization/RMA и связать возврат с конкретным
  order item, shipment и количеством.
- Поддержать полный и частичный возврат, приём на склад, inspection и
  disposition: `restock`, `quarantine`, `damaged` или отказ по policy.
- Создавать соответствующие ledger/return-receiving факты только после
  фактического приёма; повторный приём не должен удваивать остаток.
- Отдельно обрабатывать возврат до отгрузки, carrier return и потерянную
  посылку.

**Acceptance:** synthetic partial return проходит от RMA до inspection и
  disposition; quantity, warehouse и ownership проверяются tenant-scoped;
  повтор webhook/сканирования даёт тот же результат.

### 224.8 — Refund, fiscal и settlement

- Использовать Payment/Refund агрегаты Task 164; не создавать параллельную
  refund state machine.
- Формировать preview распределения по строкам, скидке, налогу, доставке и
  комиссии с exact money и причиной возврата.
- Для рискованных операций применять approval/policy; вызов payment connector
  должен быть idempotent и поддерживать `succeeded`/`failed`/`unknown`.
- Отразить refund в fiscal/settlement/reconciliation evidence и не считать его
  новой продажей или отрицательным произвольным исправлением ledger.

**Acceptance:** частичный и полный refund нельзя выполнить дважды; сумма не
  превышает оплаченный остаток; при неизвестном ответе повтор не списывает
  деньги, а создаёт задачу сверки; все изменения имеют audit/outbox lineage.

### 224.9 — Durable orchestration и компенсации

- Собрать шаги 224.2–224.8 в durable workflow/worker с lease, retry только
  для retry-safe ошибок, backoff/jitter, дедлайнами и восстановлением после
  crash.
- Реализовать безопасные компенсации: release reservation, отмена shipment,
  ручная обработка неразрешимого shortage/unknown; не компенсировать внешнее
  действие вслепую.
- Ввести inbox/deduplication для входящих webhook и command delivery.
- Сохранять состояние процесса и попытки в PostgreSQL, а события публиковать
  через outbox; Kafka не должен быть источником транзакционной истины.

**Acceptance:** worker можно остановить после каждого внешнего вызова и
продолжить без дублей; fencing защищает от двух активных исполнителей;
все зависшие процессы видны в `needs_attention` с владельцем и next action.

### 224.10 — API, SDK и рабочее место оператора

- Добавить detail/timeline endpoint для сквозного процесса с order,
  allocation, WMS task, shipment, return, refund и reconciliation links.
- Добавить безопасные команды retry/reconcile/cancel/approve там, где это
  разрешено policy; для каждой ошибки вернуть понятный код, сообщение и
  correlation ID.
- Обновить OpenAPI и сгенерированные SDK; не раскрывать SQL-модель, токены,
  raw carrier/payment payload и cross-tenant данные.
- В UI показать прогресс по этапам, миниатюру товара, статус, tracking,
  остатки, суммы возврата, причины блокировки и ручные действия.

**Acceptance:** оператор может пройти happy path и разрешить штатное
`needs_attention` из одного заказа; UI использует SDK, показывает ошибки и не
считает локальный optimistic state подтверждением внешней операции.

### 224.11 — Qualification официальных connectors

- Выбрать минимум по одному официально поддержанному connector-у marketplace,
  carrier и payment/fiscal либо явно зафиксировать capability gap.
- Для каждого проверить sandbox/test credentials, timeout/rate limits, mapping,
  idempotency, webhook signature, status translation, label/refund semantics,
  retry и reconciliation.
- Подготовить conformance matrix и evidence; unsupported capability должна
  показываться как `read_only`/`partially_supported`/`not_available`, а не как
  успешная операция.
- Запретить production claim без отдельной live qualification на целевой
  topology.

**Acceptance:** сохранены повторяемые fixtures и runtime evidence для всех
внешних вызовов; хотя бы один полный путь проходит через реальные sandbox API
либо release явно остаётся заблокированным с указанной причиной.

### 224.12 — Сверка, наблюдаемость и восстановление

- Добавить reconciliation по каждому звену: order ↔ allocation ↔ WMS ↔
  shipment/tracking ↔ return ↔ refund ↔ settlement.
- Ввести метрики latency/age, duplicate suppression, unknown, shortage,
  shipment failure, refund pending и stock/money mismatch; добавить audit trail
  и redacted structured logs.
- Описать operator runbooks для stuck workflow, потерянной этикетки,
  неизвестного refund, расхождения остатков, повторной доставки webhook и
  частичного возврата.
- Применить tenant quotas, kill-switch для внешних writes и безопасное
  восстановление после outage.

**Acceptance:** намеренно созданное расхождение появляется в очереди
reconciliation с owner/SLA; runbook позволяет повторить безопасный шаг или
закрыть exception без ручного редактирования ledger.

### 224.13 — E2E, failure matrix и release gate

- Добавить authenticated browser E2E для order detail и операторских действий,
  а также API/worker E2E в Compose с synthetic marketplace, WMS, carrier,
  payment и fiscal/settlement stubs.
- Основной сценарий: заказ из двух строк → резерв → pick/pack → label →
  dispatch/tracking → частичный возврат одной строки → inspection/disposition
  → частичный refund → reconciliation. Отдельно проверить полный refund и
  отмену до отгрузки.
- Проверить duplicate commands/webhooks, out-of-order events, worker crash,
  lease loss, timeout after remote commit, insufficient stock, expired label,
  over-refund, cross-tenant access, approval denial и rate limit.
- Добавить bounded load smoke минимум на 1 000 synthetic orders с контролем
  отсутствия дублей и утечек; результаты хранить как qualification evidence.
- Обновить docs, contract/static checks и release pipeline: repository PASS и
  environment-specific live qualification должны быть разделены.

**Acceptance:** полный synthetic golden path проходит без дублей резерва,
сборки, shipment, статуса, возврата или refund; важные сбои приводят к
`failed`/`unknown`/`needs_attention`, а не к ложному успеху. Production
готовность объявляется только при наличии v2 linked evidence на целевых
connectors и топологии. Aggregate release gate обязан связать один flow через
marketplace → carrier → payment/fiscal → returns/refund/settlement и отдельно
подтвердить marketplace compensation, Chestny ZNAK и ЭДО. Без официальных
non-production credentials и redacted evidence это остаётся внешним blocker-ом,
а не synthetic PASS.

## Не входит в этот task

- Chargebacks/disputes, кредитование и полноценный payment acquiring — отдельный
  финансовый контур.
- Новые marketplace-specific taxonomy/product features — Task 222.
- Массовый repricing, Buy Box и promotion writes — Task 221.
- Неофициальный scraping или browser automation вместо connector API.
- Переписывание уже принятых доменных агрегатов без ADR и миграционного плана.

## Зависимости

006, 054, 055, 074, 117, 164, 170, 223.

## Definition of Done

- Все 13 подзадач имеют implementation, contract/docs и success/failure/
  idempotency tests.
- `gofmt`, `go test ./...`, `go vet ./...`, `./scripts/check-contracts.sh`,
  `make sdk-check`, `make architecture`, `make migrations` проходят.
- Compose E2E и authenticated browser E2E сохранены как evidence.
- Для каждого live connector есть conformance/reconciliation evidence либо
  явно указан release blocker.
- Ручные действия, аудит, rollback, quotas и security/privacy impact описаны;
  production claim не делается по одному repository build.
