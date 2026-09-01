# Task 229 — Мобильная и складская работа: pick-листы, сканирование, печать и FBO/FBS

## Статус

`repository-complete` — desktop WMS workspace, durable allocations, pick/pack
tasks и логистические label ports уже существуют в Tasks 055, 117, 170 и 224.
Единый mobile-first процесс оператора, печать складских документов,
устойчивый scan flow и модель FBO/FBS реализованы в repository-контуре.

## Цель

Дать кладовщику один быстрый и безопасный рабочий контур:

```text
заказ/план → pick-лист → маршрут → сканирование → сборка → упаковка
→ печать этикетки/документов → handoff → tracking/status
```

И одновременно поддержать два способа исполнения:

- **FBS** — товар собирается на складе продавца, затем маркируется и передаётся
  перевозчику/marketplace;
- **FBO** — товар заранее поставляется на склад marketplace, а заказ и
  отгрузку со стороны marketplace мы только синхронизируем; локальные pick/
  pack действия не должны выдаваться за действия marketplace-оператора.

Одна canonical fulfillment model должна показывать режим, владельца запаса,
склад, allocation, shipment и текущий этап. Provider-specific различия
остаются в connector capability и mapping, без веток FBO/FBS по именам
площадок в Core.

## Что уже есть и что закрывает этот task

- Task 055 даёт state machine receiving/put-away/pick/pack/transfer/return и
  scanner-friendly API.
- Task 117 даёт durable fulfillment allocation, reservation и failover.
- Task 170 даёт desktop очередь WMS, claim/start/scan/complete/exception и
  внутренний pack handoff.
- Task 074 и Task 224 дают typed logistics label/shipment и сквозной order
  flow.

Task 229 добавляет mobile operator experience, pick-list optimization,
device/scanner/print boundaries, offline-safe sync и FBO/FBS orchestration.
Он не создаёт второй складской ledger, allocation или order lifecycle.

## Подзадачи

### 229.1 — ADR, модели исполнения и ownership

**Зависимости:** 055, 117, 170, 224.

- Зафиксировать provider-neutral `FulfillmentMode`: `fbs`, `fbo`, `hybrid`,
  `split`, а также владельца inventory, pick/pack, label, shipment и status.
- Определить, какие действия принадлежат seller warehouse, marketplace,
  carrier и pickup point; remote action нельзя показывать как локально
  выполненный.
- Утвердить canonical flow для FBS, FBO inbound, FBO order visibility,
  hybrid/split order, cancellation, return и failed handoff.
- Описать device, scanner, printer, offline и recovery boundaries, risk class,
  approval, audit и idempotency.
- Согласовать с Task 224, чтобы mobile workflow использовал те же order,
  allocation, WMS task, shipment, return и refund references.

**Acceptance:** ADR содержит state/ownership matrix и примеры FBS/FBO/hybrid;
нет перехода, который одновременно меняет local WMS и remote marketplace
status без typed capability и durable evidence.

### 229.2 — Mobile device/session и безопасный доступ

**Зависимости:** 229.1.

- Сделать mobile-first/PWA surface для handheld экранов с OIDC session,
  коротким сроком жизни, device binding и reauthorization.
- Хранить access tokens только в approved auth boundary; не класть токен,
  tenant или секрет в localStorage/service-worker cache.
- Добавить регистрацию/отзыв устройства, warehouse/zone scope, role и
  operator shift; потерянное устройство можно немедленно заблокировать.
- Включить accessibility, large touch targets, hardware keyboard, camera
  permission, dark/high-contrast mode и работу на малом экране.
- Разделить права на просмотр задания, scan, quantity correction, exception,
  print, reprint, cancel и manual override.

**Acceptance:** mobile auth/session, device revoke, tenant isolation,
  permission denial и responsive/accessibility browser tests проходят; offline
  cache не содержит токенов или лишнего customer PII.

### 229.3 — Pick-list generation, batching и маршрут

**Зависимости:** 229.1–229.2, 170.

- Генерировать pick-list из confirmed order/allocation с SKU, количеством,
  location, lot/serial/FEFO и приоритетом, не создавая новые reservations.
- Поддержать wave/batch picking, zone picking, order picking и bounded batch
  size с явным правилом приоритета.
- Оптимизировать маршрут по location graph только как derived suggestion;
  исходные warehouse locations и allocations остаются authoritative.
- Показывать FBS задачи отдельно от FBO visibility и исключать remote-owned
  stock из локального pick-list.
- Перестраивать список при shortage/reallocation только после version/lease
  check и с сохранением уже подтверждённых scans.

**Acceptance:** один заказ даёт корректный pick-list, batch не смешивает
чужие/закрытые allocations, повторная генерация стабильна, split/hybrid order
имеет отдельные строки и повтор не удваивает task.

### 229.4 — Сканирование barcode/GTIN/DataMatrix

**Зависимости:** 229.3.

- Поддержать hardware scanner и camera scan с debounce, checksum/format
  validation, expected SKU/location/quantity и явным подтверждением mismatch.
- Разделить scan product, location, package, serial/lot и label; не принимать
  один тип кода за другой.
- Передавать scan как idempotent command с task version, device/operator,
  location и exact quantity.
- Raw barcode/DataMatrix/certification payload держать только транзитно;
  в истории сохранять bounded digest и необходимые operational facts.
- Объяснять ошибки: wrong SKU, wrong location, duplicate scan, expired lot,
  over-pick, unknown code, revoked device и offline conflict.

**Acceptance:** tests для valid/invalid/duplicate/wrong-location/over-pick,
DataMatrix, hardware/camera fallback, crash и repeated command проходят;
progress не увеличивается дважды и raw scan data не попадает в Postgres/logs.

### 229.5 — Mobile pick execution и исключения

**Зависимости:** 229.3–229.4, 170.

- Сделать mobile lifecycle claim → start → scan → short/replace → complete
  с lease fencing и optimistic version.
- Поддержать partial pick, shortage, damaged, wrong location, unavailable
  item, substitution policy и escalation в supervisor/WMS exception.
- Показывать operator exact progress, next location, remaining quantity,
  priority, due time, order/item context и FBS/FBO mode.
- При отмене заказа или reroute allocation останавливать старую task без
  потери immutable scan history.
- Не считать scan фактом отгрузки и не менять on-hand вне существующего WMS
  ledger transition.

**Acceptance:** mobile happy path закрывает все строки; partial/exception/
cancel/reroute оставляют согласованные allocation/task/history; повтор после
lease loss не теряет подтверждённое количество и не создаёт двойного pick.

### 229.6 — Packing station, package facts и контроль веса

**Зависимости:** 229.5.

- Создать pack session, связанный с order/allocation/shipment, и подтвердить
  фактические SKU/quantity перед закрытием упаковки.
- Поддержать package count, dimensions, weight, packaging type, seal/serial
  facts и optional scale integration через typed device port.
- Проверять expected vs actual weight/size, dangerous/oversized rules,
  incomplete order и duplicate package.
- Дать возможность открыть/перепаковать только через audited exception;
  existing label должен стать stale при изменении package facts.
- Для FBO показать inbound package/ASN facts и не выдавать FBO remote order за
  локальную pack session.

**Acceptance:** pack нельзя закрыть при missing line/invalid quantity;
повтор scale/pack idempotent, mismatch создаёт exception, изменение package
создаёт новую label intent, а предыдущая история остаётся неизменной.

### 229.7 — Печать этикеток и складских документов

**Зависимости:** 229.1, 229.6, 074, 224.

- Создать host-owned print queue с registered printer, warehouse/zone,
  template/version, media size, language, copies и job status.
- Поддержать label, pick-list, packing slip, manifest/act и internal barcode
  print только при соответствующей capability и released artifact.
- Идемпотентно печатать по print intent/label receipt; reprint явно помечать,
  требовать permission и не создавать новую shipment/label без причины.
- Обрабатывать printer offline, paper/media error, partial print, timeout и
  duplicate job; UI должен показывать `поставлено`, `напечатано`, `ошибка`,
  `повторить` и `неизвестно`.
- Не отправлять секреты или raw customer/payment data в printer payload;
  удалённый label хранить через upload quarantine/release policy.

**Acceptance:** одна label не печатается дважды из-за retry, reprint имеет
audit, printer outage не теряет job, неизвестный результат требует проверки,
а FBO-only remote document не появляется в FBS print queue.

### 229.8 — FBS/FBO/hybrid fulfillment orchestration

**Зависимости:** 229.1, 229.3–229.7, 224.

- Ввести единый fulfillment plan с mode, owner, allocation, package,
  shipment, remote warehouse, handoff и status lineage.
- Для FBS связать order → reserve → mobile pick/pack → label → carrier/
  marketplace handoff → tracking/status.
- Для FBO связать inbound replenishment/acceptance → remote stock/order
  visibility → marketplace status/settlement; не выполнять локальный pick/pack
  после передачи ownership.
- Для hybrid/split группировать shipments и customer-visible status, не
  объединяя разные ownership/warehouses в одну ложную операцию.
- Поддержать mode change только до irreversible step и с explicit policy;
  после handoff создаётся compensation/manual attention, а не silent switch.

**Acceptance:** synthetic FBS, FBO и hybrid flows имеют корректные owner,
tasks, labels, shipments и statuses; повтор webhook/worker не дублирует
shipment, handoff, stock movement или remote status.

### 229.9 — Offline-safe очередь и синхронизация

**Зависимости:** 229.2, 229.4–229.8.

- Определить offline policy: разрешать только ограниченный preloaded task
  context и локальную очередь scan intents; внешние writes/dispatch/print
  нельзя считать выполненными без server receipt.
- Хранить на устройстве минимум данных и bounded encrypted queue; expiration,
  device revoke и remote wipe должны блокировать старые intents.
- При reconnect отправлять intents с idempotency key, task version, device
  session и sequence; обрабатывать conflict, duplicate, reorder и stale task.
- Показывать оператору pending/synced/rejected/needs_attention и не скрывать
  потерю связи под зелёным status.
- Не разрешать offline изменение reservation, on-hand, shipment state,
  refund или marketplace status без server authorization.

**Acceptance:** disconnect/reconnect, crash, duplicate queue, lease expiry,
  server conflict, device revoke и out-of-order intents не создают двойного
  scan/pack/print; неподтверждённый offline intent явно остаётся pending.

### 229.10 — API, SDK, persistence и event contracts

**Зависимости:** 229.3–229.9.

- Добавить API для mobile task feed, pick-list, scan, pack, device/printer,
  print queue, fulfillment plan, mode/status и offline intent replay.
- Все mutations требуют tenant scope, permission, `Idempotency-Key`, expected
  version и correlation ID; lists используют cursor pagination.
- Обновить OpenAPI, generated SDK, event catalog и stable capability codes для
  scanner, print, FBS/FBO и device management.
- Добавить expand-only storage для devices, scans digest, pick batches, pack
  sessions, print intents/jobs, fulfillment plans и sync conflicts.
- Включить FORCE RLS, append-only history, idempotency uniqueness, bounded
  indexes и retention policy для device/scan/print evidence.

**Acceptance:** contract parity, migration/static checks, two-tenant RLS,
idempotency/replay, append-only and safe error tests pass; SDK не открывает
неподдержанную FBO/FBS или print capability.

### 229.11 — Mobile UI и рабочее место склада

**Зависимости:** 229.2–229.10.

- Сделать mobile home с текущей сменой, складом/зоной, очередью, приоритетами,
  offline state и счётчиками исключений.
- Реализовать экраны pick-list, scan, shortage/exception, pack, label/print,
  handoff, FBO/FBS plan и история задания.
- Использовать крупные действия, минимальное число полей, вибро/звуковое
  подтверждение, аппаратную клавиатуру и доступный focus order.
- Показывать product thumbnail, SKU, expected/actual quantity, location,
  package/label status и точное русское объяснение ошибки.
- Не давать оператору «завершить» локально, если server receipt/remote
  confirmation отсутствует; показывать pending/unknown/manual attention.

**Acceptance:** authenticated browser/device tests покрывают 320–480px ширину,
keyboard/scanner input, camera permission, print error, offline banner,
FBS/FBO split, retry и отсутствие ложного success.

### 229.12 — Connector и hardware qualification

**Зависимости:** 226, 229.7–229.10.

- Для marketplace/carrier/FBO provider проверить exact capabilities: order/
  inventory read, fulfillment mode, shipment, label, tracking, handoff,
  inbound/acceptance и return.
- Для scanner/scale/printer определить supported protocol, discovery,
  pairing, health, timeout, retry и safe fallback; hardware failure не должен
  менять canonical state.
- Квалифицировать минимум один FBS connector и один FBO-capable connector с
  official API/sandbox; остальные показывать `read_only`/`not_available`.
- Сохранить conformance evidence, versions, scopes, limits, mapping,
  read-after-write и remote unknown semantics.
- Не считать работающий printer/scanner или manifest достаточным для
  `qualified` fulfillment connector.

**Acceptance:** capability matrix синхронна между manifest/runtime/API/UI;
первый FBS и FBO vertical slice проходят sandbox/read-after-write/reconcile,
а неподдержанная операция блокируется до remote call.

### 229.13 — Security, observability и recovery

**Зависимости:** 229.2, 229.4, 229.7–229.12.

- Ввести metrics: pick throughput, scan errors, shortage, pack mismatch,
  print queue age/failure, offline pending age, device health, label unknown,
  handoff lag и reconciliation drift.
- Audit actor/device/warehouse/task, scan/pack/print digest, before/after
  version, exception, reprint reason и remote receipt; raw barcode, tokens,
  customer PII и secret material исключить.
- Добавить quotas по device, operator, warehouse, print copies, offline queue,
  batch size и concurrent sessions; kill switch для remote handoff/print.
- Подготовить runbooks для lost device, printer outage, wrong scan,
  duplicate label, offline conflict, stuck FBO acceptance и FBS handoff
  unknown.
- Восстанавливать worker/device queue после crash без повторного внешнего
  эффекта; unresolved state отправлять в manual attention.

**Acceptance:** alerts и runbooks позволяют найти и безопасно повторить только
подтверждённый шаг; kill switch не скрывает evidence; security/tenant/device
negative tests проходят.

### 229.14 — Demo, E2E, нагрузка и release gate

**Зависимости:** все предыдущие подзадачи.

- Добавить synthetic orders, allocations, warehouse locations, FBS/FBO/hybrid
  plans, pick batches, valid/invalid scans, packages, printers, labels,
  handoffs, returns и exceptions.
- Пройти authenticated E2E: FBS order → pick-list → mobile scans → shortage
  recovery → pack → print label/packing slip → handoff → tracking.
- Пройти FBO inbound → acceptance → remote order visibility и hybrid order с
  FBO/FBS shipments; локальный UI не должен показывать FBO remote pick task.
- Проверить duplicate scan, duplicate print, crash after remote accept, offline
  reconnect, lease loss, wrong warehouse, revoked device, printer outage,
  stale allocation, cross-tenant и capability denial.
- Добавить bounded load smoke минимум на 1 000 order/scan/print intents с
  контролем дублей, queue lag и memory; результаты хранить как evidence.
- Release разрешает production claim только при актуальном conformance/live or
  sandbox evidence для выбранных FBS/FBO connectors и hardware profile.

**Acceptance:** synthetic FBS/FBO/hybrid paths проходят без двойных scan,
reservation, package, label, print job, shipment или handoff; все unknown/
offline/exception состояния видны оператору, а production readiness имеет
retained evidence.

## Архитектурные ограничения

- WMS/Inventory/Order/Fulfillment/Logistics остаются canonical bounded
  contexts; mobile — интерфейс и command transport, не новый ledger.
- PostgreSQL хранит операционную истину, Outbox/Inbox защищают события и
  команды, device/offline queues не являются authority.
- FBO/FBS — capability/ownership attributes, а не provider-name branches в
  Core. Remote marketplace state не считается локально выполненным без receipt.
- Сканируемые коды, токены, raw labels и customer/payment data не должны
  попадать в обычные columns, events, logs, device cache или fixtures.
- Все внешние writes tenant-scoped, idempotent, approval/policy-gated и
  fail-closed при timeout/unknown; print/reprint не должен незаметно создавать
  новый shipment.
- AI/MCP/n8n не могут завершить pick/pack, подтвердить print/handoff или
  обойти device, warehouse, quantity и approval controls.

## Не входит в этот task

- Автономные роботы, computer vision quality inspection и полноценный WMS
  slotting optimizer.
- Разработка собственного hardware scanner/scale/printer.
- Полный FBO execution внутри marketplace warehouse; мы синхронизируем только
  официально доступные remote facts и действия.
- Новые marketplace taxonomy/product features, repricing, promotions,
  returns/refund domain или accounting ledger.

## Зависимости

054, 055, 074, 117, 164, 170, 217, 223, 224, 226.

## Definition of Done

- Все 14 подзадач имеют implementation, contracts/docs и success/failure/
  idempotency/offline tests.
- Mobile pick/pack/scan/print и FBS/FBO/hybrid orchestration доступны через
  единые API/SDK/UI capability guards.
- Включены RLS, audit, device security, raw-code redaction, quotas, recovery,
  reconciliation, demo data и authenticated E2E.
- Для каждой включённой FBS/FBO операции есть connector/hardware evidence;
  unsupported operations честно остаются `read_only` или `not_available`.
- Пройдены `gofmt`, `go test ./...`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `make architecture`, `make migrations`,
  frontend typecheck/build и connector conformance на целевой topology.

## Repository result

Закрыты 229.1–229.11 и repository-части 229.13–229.14: добавлены ADR,
expand-only migration 000056, FBS/FBO/hybrid plan policy, device registry,
pick batches поверх canonical WMS tasks, digest-only scan evidence, exact pack
facts, print queue, offline reconnect receipts, optimistic stage advance,
OpenAPI/Go/Python/TypeScript SDK, versioned events, RLS/audit, frontend
`/warehouse/mobile`, static qualification gate и эксплуатационный runbook.

Остался только внешний release-gate 229.12/229.14: нужны credentialed
sandbox/live проверки выбранных FBS/FBO marketplace/carrier connector-ов и
конкретных scanner/camera, scale и printer profiles на целевой topology. До
появления redacted evidence эти capabilities честно остаются
`read_only`/`partially_supported`/`qualification_required`; repository-complete
не означает production-qualified.
