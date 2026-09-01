# Task 164 — Возвраты, отмены и refunds

## Status

`repository-complete` — provider-neutral доменные агрегаты, миграция PostgreSQL,
outbox/audit, идемпотентный API, generated SDK, durable logistics route и
операторский экран с созданием отмены/возврата, строками, этикеткой, инспекцией
и refund allocation реализованы и проверены. Live connector/WMS/fiscal/
settlement qualification и production enablement остаются отдельным release
gate; внешние credentials и неподтверждённые результаты в этот статус не
включены.

Репозиторная проверка: `go test ./...`, `go vet ./...`, contract/architecture/
migration gates и frontend typecheck/build/logic/docs проходят. Полный production
claim не делается до live Compose/conformance прогона.

### Repository completion evidence — 2026-09-01

- API и OpenAPI/SDK покрывают cancellation, return, line item, status,
  inspection, carrier return operation и refund allocation.
- `frontend/src/pages/ReturnsPage.tsx` предоставляет оператору создание
  отмены/возврата, добавление строки, запрос возвратной этикетки и резерв
  refund allocation; raw provider payloads не показываются.
- PostgreSQL использует tenant scope, optimistic version, idempotency,
  append-only history и transactional outbox. Неизвестный внешний результат
  остаётся `unknown`/manual attention и не повторяется вслепую.
- Live provider, WMS, fiscal и settlement evidence не выдумывается и должна
  быть приложена к release gate отдельно.

## Objective

Довести до production-ready единый provider-neutral контур отмен заказов,
товарных возвратов и возврата денег (refund). Контур должен связывать заказ,
строки заказа, оплату, shipment/перевозчика, резервы и складской ledger, но не
сливать их в один неуправляемый статус.

Нужно поддержать полную и частичную отмену/возврат, несколько refund на одну
оплату, возврат доставки и налогов по утверждённой политике, приём и инспекцию
товара, разные disposition (restock/quarantine/scrap/replace), а также
reconciliation после timeout или webhook от внешнего провайдера. Существующий
`payments.Refund` и `POST /payments/{payment_id}/refund` остаются совместимым
нижним уровнем; новая модель должна добавить связь с order/return и инварианты,
не создавая второй путь изменения платёжного состояния.

## Target end-to-end slice

Полное закрытие задачи проверяется не по наличию отдельных endpoint или таблиц,
а по одному воспроизводимому сценарию для заказа с одной или несколькими
строками:

1. **Заказ принят:** заказ импортирован с immutable snapshot цены, налога, SKU и
   количества; повторный импорт не создаёт дубль.
2. **Резерв создан:** доступное количество зарезервировано атомарно в рамках
   tenant/workspace; повторная команда не уменьшает остаток второй раз.
3. **Сборка выполнена:** WMS создаёт picking/packing task, оператор может
   подтвердить частичную сборку, а остатки и статусы остаются согласованными.
4. **Этикетка получена:** через заявленный logistics capability создаётся
   shipment/label и сохраняется только безопасная проекция remote receipt;
   timeout не превращается в ложный успех.
5. **Отгрузка подтверждена:** order/shipment transitions, tracking и статус
   канала обновляются через typed connector port; webhook и worker replay
   идемпотентны.
6. **Возврат обработан:** создаётся полный или частичный return, затем
   `received -> inspecting -> accepted|partially_accepted|rejected`, после чего
   disposition (`restock|quarantine|scrap|replace`) отражается отдельным WMS
   ledger movement.
7. **Деньги возвращены:** создаётся refund preview и allocation по строкам,
   доставке и налогу; после policy/approval внешний refund проходит через
   существующий payment aggregate, а settlement/fiscal evidence связывается с
   исходным заказом.
8. **Итог сверяем:** reconciliation подтверждает remote status или переводит
   операцию в `unknown`/`manual_attention`; оператор видит timeline, причину,
   retry-safe действие и audit evidence.

### End-to-end acceptance gate

Сценарий считается закрытым только если он проходит для полного и частичного
возврата в Compose с synthetic marketplace/store, payment, carrier и WMS
stub-ами. Обязательны проверки duplicate command/event/webhook, crash до и
после remote acceptance, потеря lease, provider timeout/rate-limit,
out-of-order webhook, over-refund, cross-tenant ID, mismatch валюты и
недоступная capability. Ни один из этих случаев не должен создавать двойной
резерв, двойной refund, лишнее движение склада или тихую перезапись факта.

## Architecture boundaries

- `Order` и `OrderItem` остаются коммерческим snapshot: их прошлые цены,
  налоги, SKU и quantity не переписываются. Отмена — отдельная команда и
  append-only evidence, а итоговый order status меняется только через
  существующий `orders.ValidateTransition`/repository boundary.
- Возврат, отмена и refund — отдельные агрегаты/состояния. Нельзя считать
  созданный refund доказательством принятого товара или автоматически менять
  склад до подтверждения receipt/inspection.
- PostgreSQL — источник истины для request, allocation, state history, leases,
  mapping и reconciliation evidence; Kafka получает только canonical events
  через Transactional Outbox. Inbox/deduplication защищают consumer и webhook
  от повторной доставки.
- Все внешние вызовы идут через capability-specific connector ports и host
  runtime: timeout, rate-limit budget, idempotency key, normalized errors и
  read-after-write/reconciliation. Core не ветвится по `ozon`, `cdek` и другим
  provider id.
- `write_sensitive` и `legally_significant` отмены/refunds/fiscal corrections
  используют Task-017 policy/approval. После approval все scope, capability,
  account status, amount и version проверяются заново.
- Деньги хранятся как integer minor units + currency, quantity — exact decimal.
  Суммарный refundable amount не может превышать захваченную оплату минус уже
  успешные/зарезервированные refund; cross-currency и округление запрещены без
  явной sourced FX/rounding policy.
- В state/evidence/logs/events хранятся только typed references, hashes,
  remote IDs и bounded machine reason codes. Сырые provider payloads, токены,
  Authorization headers, платёжные реквизиты и лишний PII не сохраняются.

## Canonical lifecycle to approve in the ADR

Точные имена и additive-совместимость должны быть закреплены в 164.1, но
реализация обязана разделять как минимум следующие переходы:

- cancellation: `requested -> approved? -> executing -> cancelled | rejected |
  failed | unknown`; `unknown` означает неоднозначный внешний результат и
  требует reconciliation/manual attention, а не слепого повторения;
- return: `requested -> approved? -> authorized -> in_transit -> received ->
  inspecting -> accepted | partially_accepted | rejected -> closed`, плюс
  `cancelled`/`expired` до необратимой стадии;
- refund: совместить текущие `pending -> accepted -> succeeded | failed` с
  явным представлением `unknown`/`manual_attention` для timeout и
  неразличимого результата, не превращая его в ложный `failed`;
- line allocation: одна строка может возвращаться частями, но принятая
  quantity и refund allocation никогда не превышают исходную quantity/сумму;
- disposition: `restock`, `quarantine`, `scrap`, `replace` — это отдельная
  складская операция после inspection, а не поле, которое тихо меняет
  `inventory_position.on_hand`.

## Subtasks and implementation order

### 164.1 — ADR, scope, terminology and policy matrix

**Depends on:** none.

- Зафиксировать ADR для cancellations/returns/refunds и границы первой версии.
- Утвердить словарь `cancellation`, `return`, `return_item`, `refund`,
  `refund_allocation`, `shipment`, `receipt`, `inspection`, `disposition`,
  `evidence`, `unknown outcome` и их владельцев.
- Составить матрицу этапов заказа: до оплаты, после capture, до передачи
  перевозчику, в пути, после доставки и после частичного возврата.
- Определить, какие действия auto-safe, какие требуют approval, а какие всегда
  manual/blocked; отдельно описать ограничения по суммам, валюте, сроку,
  причине, legal entity, fiscal receipt и возврату доставки.
- Согласовать совместимость с Task 006 orders, Task 017 approval, Task 059
  settlement, Task 071 fiscalization, Task 074 logistics и Task 138 payment
  reconciliation; явно исключить chargeback/dispute из первой версии.

**Acceptance:** ADR и policy matrix одобрены; нет неоднозначности между
отменой, товарным возвратом, refund, chargeback и корректировкой фискального
чека; для каждого перехода указан owner и требуемое evidence.

### 164.2 — Canonical domain contracts and state machines

**Depends on:** 164.1.

- Ввести provider-neutral типы `CancellationRequest`, `ReturnRequest`,
  `ReturnItem`, `RefundAllocation`, `InspectionResult`, `Disposition` и
  `OperationEvidence` с tenant/order/order-item/payment/shipment references.
- Не менять immutable order snapshot; хранить requested/accepted quantities,
  reason code, customer/operator source, evidence references, delivery/tax
  components и optimistic version отдельно.
- Реализовать строгие transition validators для cancellation, return, refund и
  inspection; terminal states необратимы, кроме документированного manual
  correction/adjustment path.
- Ввести инварианты: неположительная quantity/amount запрещена, currency
  совпадает с payment, duplicate allocation невозможна, accepted quantity не
  больше shipped quantity и все изменения имеют correlation/causation.
- Сохранить compatibility adapters для существующего `payments.Refund` и его
  API; новая связь не должна обходить `ValidateRefundTransition`.

**Acceptance:** domain tests покрывают полную/частичную отмену и возврат,
несколько refund, запрещённые backward transitions, over-refund, mismatched
currency, duplicate line allocation и повторное применение одной команды.

### 164.3 — Order/payment/fulfillment/inventory orchestration contract

**Depends on:** 164.2.

- Описать единую orchestration policy: когда отмена освобождает reservation,
  когда вызывает shipment cancel, когда требует return label, а когда только
  создаёт refund request.
- Разделить `order cancelled`, `shipment cancelled`, `return received`,
  `refund succeeded` и `fiscal correction`; одно событие не должно имитировать
  остальные.
- Добавить typed ports для order cancellation, carrier return/cancel,
  payment refund, fiscal refund/correction, customer notification и WMS ledger.
- Для каждой операции определить compensation/manual path; никакой
  distributed transaction и никаких silent rewrites current inventory.
- Проверить settlement/ledger semantics: refund создаёт adjustment/ledger fact,
  а не удаляет продажу или комиссию; allocation комиссии/доставки описать
  отдельной policy.

**Acceptance:** review-последовательности для `cancel before shipment`,
`cancel after capture`, `return after delivery`, `partial return` и
`refund-only` имеют однозначный результат и не допускают двойного освобождения
резерва или двойного финансового факта.

### 164.4 — PostgreSQL schema, RLS and append-only evidence

**Depends on:** 164.2, 164.3.

- Добавить expand-only migration для cancellation requests, returns,
  return_items, refund_allocations, state history, inspection/disposition,
  external operation receipts, reconciliation checkpoints и manual attention.
- Для tenant-owned таблиц включить `FORCE ROW LEVEL SECURITY`, composite
  organization/workspace predicates, optimistic versions и append-only guard
  для history/evidence.
- Уникальности и индексы: tenant + idempotency key, order + open operation,
  return + line, payment + remote refund id, due/active state + updated_at,
  reconciliation queue и bounded operator queries.
- Не дублировать полный order/payment snapshot и raw webhook body; хранить
  digest, typed amount/quantity, remote IDs, actor, policy/approval digest и
  bounded error code.
- Добавить retention/archive правила: финансовое/audit evidence дольше
  операционного run state; legal hold не удаляется обычной очисткой.

**Acceptance:** fresh install, upgrade rehearsal, migration static checks,
двухтенантный RLS smoke, negative cross-tenant tests, duplicate-key tests и
append-only history tests проходят; `EXPLAIN` подтверждает bounded plans.

### 164.5 — Canonical events, Outbox/Inbox and verified webhooks

**Depends on:** 164.2, 164.4.

- Согласовать и зарегистрировать versioned events, например
  `commerce.orders.cancellation_requested.v1`,
  `commerce.orders.cancellation_state_changed.v1`,
  `commerce.returns.requested.v1`, `commerce.returns.state_changed.v1`,
  `commerce.payments.refund_requested.v1` и
  `commerce.payments.refund_state_changed.v1`.
- Каждый event обязан использовать canonical envelope с event/occurred,
  tenant, correlation/causation, entity и source; secret/PII не допускаются.
- В одной транзакции записывать domain mutation, audit и Outbox; consumers
  используют Inbox/deterministic receipt и не создают второй refund при replay.
- Подключить существующую verified public webhook boundary для payment/order/
  return callback: tenant/account resolution и signature/provider re-verification
  обязательны, статус из тела без проверки не доверяется.
- Установить idempotency contract для UI, API, worker и provider webhook;
  повторная доставка должна возвращать безопасный acknowledgement.

**Acceptance:** schema/catalog/contract fixtures проходят; duplicate event,
duplicate webhook, out-of-order state и crash между DB commit и Kafka publish
не создают повторного side effect.

### 164.6 — Cancellation worker and remote side-effect execution

**Depends on:** 164.3–164.5.

- Реализовать durable job/lease/inbox route для cancellation requests по
  существующему PostgreSQL scheduler/worker pattern; после narrow claim снова
  применить tenant scope.
- Перед вызовом перечитать order, shipment, payment, approval, capability,
  account status и current version; stale/cancelled lease не может завершить
  чужую операцию.
- Вызовы carrier/order/payment выполнять через capability ports с timeout,
  per-tenant concurrency, deterministic idempotency key и normalized errors.
- Retry только для явно retry-safe transient ошибок; permanent/invalid/unknown
  переходят в manual attention и reconciliation, без blind retry.
- Безопасно обрабатывать crash до/после remote accept и повторный запуск после
  lease loss; освобождение reservation и локальный transition должны быть
  idempotent.

**Acceptance:** E2E synthetic tests покрывают cancel до shipment, после capture,
после передачи carrier, provider outage, timeout/unknown, duplicate command,
worker crash и stale lease; результат каждой операции виден в history/evidence.

### 164.7 — Return authorization, logistics, receipt and WMS disposition

**Depends on:** 164.3–164.6.

- Реализовать return authorization с проверкой срока/причины/quantity и
  policy; частичные строки и несколько parcel/shipments поддержать без
  изменения order snapshot.
- Через logistics connector создать/отменить return shipment и получить
  label/tracking только при заявленной capability; unsupported capability
  переводит операцию в manual, не в fictitious success.
- Принять scan/receipt и провести bounded inspection: quantity, condition,
  discrepancy, evidence/artifact refs; клиентские uploads остаются quarantine
  до security policy.
- После inspection записать WMS ledger movement: restock, quarantine, scrap или
  replacement; reservation/release и available stock пересчитать append-only.
- Защититься от повторного receipt/inspection и от возврата quantity больше
  отправленной/принятой; discrepancies требуют approval/manual resolution.

**Acceptance:** Docker synthetic carrier/store/WMS flow подтверждает
`request → authorize → in_transit → received → inspecting → disposition` для
полного и частичного возврата, inventory conservation и duplicate-scan safety.

### 164.8 — Refund orchestration, fiscalization and reconciliation

**Depends on:** 164.2–164.7.

- Расширить существующий refund route так, чтобы refund ссылался на order/
  return/allocation и проходил policy/approval до внешнего вызова; сохранить
  текущую idempotency и backward-compatible response.
- Рассчитать refund preview по line price, discount, tax, shipping, commission и
  currency с exact arithmetic; запретить сумму выше captured/available balance.
- Вызвать `PaymentProvider`/acquirer через host runtime; результат `accepted`,
  `succeeded`, `failed` и `unknown` маппить на canonical state без raw error.
- Связать успешный refund с Task-069 fiscal refund/correction и Task-059
  settlement adjustment только через их порты; повторная fiscal/ledger запись
  должна быть idempotent.
- Добавить bounded periodic reconciliation по payment/refund/return windows,
  используя Task-138 semantics: remote-authoritative status, no blind reissue,
  provider outage isolated per account и manual attention для ambiguity.

**Acceptance:** tests на full/partial/multiple refund, tax+shipping, duplicate
request, over-refund, timeout after provider accept, webhook-before-response,
reconciliation replay, fiscal/settlement failure и multi-currency проходят;
локальный payment status меняется только через canonical payments transitions.

### 164.9 — REST/OpenAPI and operator/customer UI

**Depends on:** 164.2, 164.5, 164.6–164.8.

- Добавить tenant-scoped endpoints для create/list/detail cancellation и
  return, line-level authorization/inspection, refund preview/commit,
  approve/reject, retry/manual resolution и bounded reconciliation status.
- Все mutations требуют `Idempotency-Key` и optimistic version; tenant/workspace
  нельзя принимать как доверенное поле клиента; cursor pagination обязательна
  для списков и history.
- Обновить OpenAPI, generated SDK, permission matrix и audit/event catalog;
  явно разделить `refund requested` и `refund succeeded` в ответах.
- В order detail показать действия «Отменить», «Оформить возврат», «Вернуть
  оплату» только при актуальной capability/policy; дать выбор строк/quantity,
  причины, доставки, tax, evidence и approval.
- Добавить timeline операции, expected next step, manual attention, retry
  safety и ссылки на audit/fiscal/settlement evidence; не показывать raw
  provider response или секреты.

**Acceptance:** API/UI позволяют создать idempotent partial return, получить
понятную policy/validation ошибку, пройти approval, увидеть receipt/refund
timeline и безопасно повторить запрос; unsupported connector operation не
выглядит как доступная кнопка.

### 164.10 — Connector capability qualification matrix

**Depends on:** 164.3, 164.6–164.9.

- Для каждого admitted connector отдельно квалифицировать `orders.cancel`,
  `returns.create/read`, `logistics.return.create/cancel/track`,
  `payments.refund`, fiscal refund/correction и webhook/reconciliation.
- Разделить SDK contract, runtime route и live/Docker evidence; manifest alone
  не делает операцию production-ready.
- Добавить deterministic mock fixtures для idempotency, partial refund,
  rate-limit, timeout, out-of-order webhook и remote accepted/local commit loss.
- Для unsupported/health-only providers fail closed: capability нельзя включить
  через API, UI или workflow automation.
- Сохранить provider-native IDs только в mapping/evidence и подготовить
  qualification reports для WooCommerce, OpenCart, PrestaShop, Bitrix24,
  marketplaces, payment rails и carriers по фактически заявленным операциям.

**Acceptance:** generated matrix совпадает с runtime support; каждая
production-capable операция имеет conformance + Docker/live evidence, а
неподтверждённая остаётся явно `qualification_required`.

### 164.11 — Security, observability, quotas and operational recovery

**Depends on:** 164.4–164.10.

- Метрики: cancellation/return/refund age, approval wait, unknown outcomes,
  reconciliation lag, provider latency/rate-limit, retry/DLQ, manual-attention,
  duplicate suppression, quarantine stock и per-tenant worker saturation.
- Structured logs и traces содержат только opaque IDs, policy/version,
  correlation/causation и machine error code; PII/artifact URLs redacted.
- Ввести per-workspace limits на active returns, refund amount/rate,
  concurrent remote calls, webhook body, retries и retention; breach должен быть
  tenant-local и предсказуемым на малой Compose VPS.
- Подготовить runbook: зависший refund, unknown provider outcome, rejected
  inspection, duplicate webhook, stuck shipment, quarantine discrepancy,
  approval expiry, replay с новой operation identity и emergency pause.
- Провести threat model: authorization bypass, cross-tenant IDs, replay,
  duplicate financial effect, SSRF через return label/artifact и утечка PII.

**Acceptance:** dashboards/alerts различают pending/accepted/succeeded/unknown,
manual и provider outage; quota/kill-switch не затрагивает другие tenants;
runbook содержит проверяемые recovery steps без ручного SQL исправления фактов.

### 164.12 — Tests, Compose qualification and documentation

**Depends on:** all previous subtasks.

- Добавить Go unit/integration tests для validators, repositories, RLS,
  idempotency, event schemas, API, worker, payment/fiscal/ledger adapters и
  connector conformance.
- Провести Compose E2E на synthetic PostgreSQL/Kafka/worker и disposable
  stores/carrier/payment stubs: API → outbox → Kafka → worker → remote →
  reconciliation, включая crash points и redelivery.
- Выполнить load profile для burst заказов, partial returns, scheduler catch-up,
  webhook storms и provider throttling; подтвердить bounded memory, DB pool и
  Kafka lag на small-VPS profile.
- Добавить screenshots/операционные сценарии для UI, документацию API/events,
  migration/env/runbook и таблицу фактических connector capabilities.
- Зафиксировать retained qualification evidence и release gate; до PASS не
  переводить refund/cancel/return операции в production support.

**Acceptance:** проходят `go test ./...`, `go vet ./...`, contract,
architecture, migration, frontend, conformance, performance и Compose E2E
checks; документация, generated catalogs и UI соответствуют действующему
runtime, а тестовые данные synthetic и не содержат production PII.

## Suggested delivery slices

1. **Foundation and invariants:** 164.1–164.4 — ADR, domain contracts,
   orchestration policy, schema/RLS/evidence.
2. **Safe operational core:** 164.5–164.8 — events, cancellation worker,
   return/WMS flow, refund/fiscal/reconciliation vertical slice.
3. **Operator surface:** 164.9–164.11 — API/UI, connector admission,
   observability, quotas and recovery.
4. **Release qualification:** 164.12 — tests, Docker/Compose, load/chaos,
   screenshots, docs and retained evidence.

## Explicit exclusions

- chargeback/dispute representment, BNPL-specific settlement and card-network
  dispute workflows; they require separate policy/ADR;
- automatic restock, refund or cancellation when receipt, authorization,
  capability, policy or remote outcome is not verified;
- provider-specific branches or fields in Core, direct SQL/HTTP/secrets from
  workflow/connector payload, browser scraping and arbitrary operator scripts;
- silent rewriting/deletion of order, payment, inventory, fiscal or settlement
  facts; corrections are append-only adjustment records;
- blind retry of an ambiguous external create/cancel/refund operation;
- promotion of an SDK-only or health-only connector to production capability
  without runtime route and current qualification evidence.

## References

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/02-domain-model.md`
- `docs/03-module-boundaries.md`
- `docs/04-event-platform-kafka.md`
- `docs/05-database.md`
- `docs/08-sync-reconciliation.md`
- `docs/14-observability.md`
- `docs/24-workflow-approval.md`
- `docs/46-sre-performance-slo.md`
- `adr/0009-transactional-outbox.md`
- `adr/0018-slo-performance.md`
- `adr/0041-approval-engine-policy-evidence.md`
- `adr/0057-wms-inventory-ledger.md`
- `adr/0062-settlement-payment-reconciliation.md`
- `adr/0069-provider-neutral-fiscalization.md`
- `adr/0071-payment-sbp-provider-boundary.md`
- `adr/0072-logistics-carrier-sdk.md`
- `adr/0073-pudo-state-machine.md`
- `tasks/issues/006-orders.md`
- `tasks/issues/017-approval-engine.md`
- `tasks/issues/071-kkt-ofd-fiscalization-abstraction.md`
- `tasks/issues/074-logistics-carrier-sdk.md`
- `tasks/issues/073-payments-sbp-provider-sdk.md`
- `tasks/issues/087-reference-acquiring-connector.md`
- `tasks/issues/094-woocommerce-connector.md`
- `tasks/issues/096-opencart-connector.md`
- `tasks/issues/097-bitrix24-crm-connector.md`
- `tasks/issues/138-payment-reconciliation-worker.md`
