# Task 170 — Рабочее место WMS-оператора и marketplace fulfillment

## Статус

`done` — вертикальный repository-срез 170.1 → 170.7, 170.9 и 170.12
завершён. Live marketplace writes и production qualification остаются
отдельными gate.

## Цель

Связать canonical order и fulfillment allocation с durable WMS tasks, чтобы
оператор мог получить очередь, забрать задачу, начать работу, сканировать
товар и закрыть или эскалировать исключение. Первая версия provider-neutral и
ориентирована на FBS-подобный pick-flow; полноценные marketplace orders,
этикетки, ChZ и shipment остаются отдельными задачами.

## Подзадачи

### 170.1 — ADR, границы и policy matrix

- Зафиксировать владельцев Order, Inventory/Fulfillment и WMS Execution.
- Утвердить состояния task, claim semantics, optimistic version, idempotency и
  правила exception/cancel.
- Отделить текущий repository-complete allocation от будущих marketplace
  order/shipment connector capabilities.

**Acceptance:** ADR-0117 и этот task описывают границы, инварианты,
неподдерживаемые операции и критерии rollback без provider branches.

### 170.2 — Durable WMS execution model

- Добавить tenant-scoped task, immutable task event и индексы очереди.
- Связать task с order/order item/allocation, warehouse и exact quantity.
- Включить forced RLS, append-only history, idempotency uniqueness и
  Transactional Outbox/audit integration.

**Acceptance:** PostgreSQL repository сохраняет и читает task только в
  tenant scope, повтор команды не удваивает эффект, version conflict не теряет
  сканирование.

### 170.3 — Operator API and SDK

- Добавить list/get/history и lifecycle commands под `/api/v1`.
- Проверять authn → tenant scope → permission (`wms.read`/`wms.write`),
  bounded input, idempotency и optimistic version.
- Обновить OpenAPI и сгенерировать все public SDK artifacts.

**Acceptance:** route parity, contract checker и SDK drift check видят каждую
операцию; API не раскрывает SQL-модель, секреты или cross-tenant данные.

### 170.4 — Order → allocation → pick task

- Добавить атомарную команду создания pick tasks из заказа для выбранного
  warehouse.
- Для каждой строки использовать существующий allocation invariant и ATP;
  task должен ссылаться на созданную или уже matching allocation.
- Опубликовать allocation/task changes через outbox и сделать replay по
  batch idempotency key безопасным.

**Acceptance:** успешный заказ даёт по одной task на строку, insufficient ATP,
inactive warehouse и terminal order откатывают batch, повтор возвращает тот же
результат.

### 170.5 — Task context, locations and scan traceability

- Уточнить provider-neutral контекст складского задания: warehouse, SKU,
  source/target location и exact quantity; location code не должен становиться
  remote marketplace identifier.
- Сохранять для сканирования только bounded operational facts: SHA-256 digest
  штрихкода, location code, количество, actor и время. Raw barcode/DataMatrix,
  токены и payload внешнего провайдера не сохраняются.
- Добавить fail-closed валидацию допустимых типов задания, длины location,
  единицы измерения и отсутствия переполнения/отрицательного прогресса.

**Acceptance:** task/history API отдаёт достаточно данных для работы оператора,
но не раскрывает исходный код сканирования; повтор scan с тем же
`Idempotency-Key` не меняет прогресс второй раз.

### 170.6 — Receiving, put-away and cycle-count execution

- Открыть единый lifecycle API для уже заложенных типов `receiving`,
  `put_away`, `cycle_count`, `transfer` и `return_receiving`, не создавая
  отдельные provider-specific ветки.
- Добавить безопасное создание standalone task с idempotency и initial
  immutable event; команда не списывает on-hand автоматически.
- Для receiving/put-away/cycle-count проверять source/target context и
  завершать задачу только после полного exact quantity.

**Acceptance:** оператор может создать и пройти bounded task без заказа;
ошибка версии, assignment, quantity или terminal state оставляет PostgreSQL
и history согласованными.

### 170.7 — Work batches and pack handoff

- Добавить provider-neutral batch grouping для связанных pick/pack tasks с
  атомарным созданием, bounded size и idempotent replay.
- Показывать в batch только ссылки на внутренние task IDs и состояния; не
  добавлять marketplace labels, shipment confirmation или remote status push.
- Передачу в pack оформить как локальный lifecycle handoff с audit/outbox
  evidence; on-hand consumption остаётся отдельной capability-задачей.

**Acceptance:** одна batch-команда безопасно повторяется, не включает
незавершённые/чужие tasks и не создаёт внешних marketplace side effects.

### 170.9 — Operator workspace UI

- Добавить в раздел «Остатки» очередь WMS-задач с фильтрами по state/type,
  обновлением и открытием detail drawer.
- Дать оператору claim/start/scan/complete/exception/cancel с отображением
  версии, exact progress, assignment и immutable history.
- Добавить форму создания standalone receiving/put-away/cycle-count task и
  batch/pack handoff только после появления соответствующего API.

**Acceptance:** UI использует сгенерированный SDK, не хранит токен или tenant в
browser storage и показывает понятные ошибки version/assignment/permission.

### 170.12 — Qualification, docs and evidence

- Добавить unit/static/contract checks для новых WMS contracts, route parity,
  idempotency, RLS/append-only boundaries и UI smoke coverage.
- Обновить `docs/39-wms.md`, `docs/operations/055-wms-execution.md` и короткий
  стабильный qualification summary; подробные runtime/release logs хранить в
  CI artifacts или исключённой `qualification/evidence/`.
- Зафиксировать rollback: новые WMS task/batch записи остаются читаемыми,
  незавершённые задания переводятся в manual attention, старый order flow не
  требует marketplace write API.

**Acceptance:** полный repository gate проходит; в summary явно разделены
repository PASS и environment-specific production qualification.

## Не входит в эту задачу

WB/Ozon orders write, labels, barcode/GTIN master data, Честный знак,
aggregation, shipment confirmation, marketplace status pushes и автоматическое
списание on-hand. Batch/pack здесь остаётся внутренней передачей между WMS
задачами и не является marketplace shipment.

## Проверки

`gofmt`, `go test ./...`, `go vet ./...`, `./scripts/check-contracts.sh`,
`make sdk-check`, `make architecture`, `make migrations`.
