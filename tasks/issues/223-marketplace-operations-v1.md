# Epic 176 — Marketplace Operations v1

## Статус

`in_progress` — родительский integration-gate для полного marketplace-сценария.

Репозиторный ключ этой задачи — `223`: ключи `176–222` уже заняты
существующими задачами. Пользовательский номер Epic сохраняется как `176`;
дублирующую нумерацию в backlog не используем.

Этот Epic не создаёт вторые модели товаров, заказов, складских остатков,
возвратов, маркировки или финансов. Он связывает уже существующие домены и
фиксирует критерий, после которого кабинет можно назвать полноценной
marketplace-интеграцией.

## Цель

Довести TORGNEXA от набора отдельных read/partial surfaces до проверяемого
операционного цикла:

```text
подключение кабинета → публикация товара → цена/остаток → заказ
→ резервирование → сборка → отгрузка → возврат
→ settlement → P&L
```

Для каждого кабинета capability matrix должна показывать не намерение или
наличие метода в SDK, а реально допущенную операцию, её auth scope,
idempotency, read-after-write, unknown/reconciliation semantics и qualification
evidence.

## Текущее состояние и границы

| Область | Основа в репозитории | Что ещё не означает готовность |
|---|---|---|
| Каталог и публикация | [Task 217](217-marketplace-product-publication.md) | Публикация товара сама по себе не закрывает цены, заказы и fulfillment. |
| Поставщики и закупки | [Task 218](218-supplier-procurement-operations.md) | Контур закупки не является импортом marketplace-заказов. |
| Финансы продавца | [Task 219](219-seller-financial-analytics.md) | P&L принимает подтверждённые факты, но полный marketplace fact flow ещё нужно квалифицировать. |
| Реклама | [Task 220](220-marketplace-advertising-runtime.md) | WB/Ozon имеют read-only MVP; управление кампаниями не включается этим Epic автоматически. |
| Возвраты | [Task 164](164-returns-cancellations-refunds.md) | Полный live WMS/carrier/payment/settlement slice остаётся отдельным release gate. |
| Маркировка и УПД | [Epic 171](171-marking-execution-and-upd.md) | Синтетическая квалификация не заменяет live qualification Честного знака и ЭДО. |
| Цены и карточки | [Task 221](221-marketplace-pricing-repricing-promotions.md), [Task 222](222-marketplace-listing-content-attributes.md) | Это downstream work packages; их наличие в backlog не означает runtime readiness. |

До прохождения полного gate WB, Ozon и остальные каналы должны показываться как
`read_only` или `partially_supported`, если соответствующий набор операций не
имеет qualification evidence. Manifest, SDK-типы, health-check или synthetic
fixture не могут сами по себе поднять статус до `qualified`.

## Подзадачи

### 176.1 — Definition of Done marketplace

- [ ] Описать lifecycle от подключения кабинета до P&L и возврата.
- [ ] Составить capability matrix для WB, Ozon, Yandex Market и остальных
  marketplace.
- [ ] Для каждой операции зафиксировать auth scope, risk class, idempotency,
  retry policy, read-after-write, timeout/unknown и reconciliation evidence.
- [ ] Разделить `read_only`, `partially_supported` и `qualified` в каталоге,
  UI и runtime support.

### 176.2 — Подключение marketplace-кабинета

- [ ] Добавить создание и настройку tenant-scoped marketplace account.
- [ ] Проверять credentials и scopes отдельным bounded health/auth flow.
- [ ] Сохранять выбранный магазин/склад и capability-aware settings.
- [ ] Показывать состояния `configured`, `healthy`, `degraded` и
  `reauthorization_required` с причиной и временем наблюдения.
- [ ] Хранить credentials только через SecretProvider; токены не попадают в
  API, логи, events, audit metadata или обычные SQL columns.

### 176.3 — Управление каталогом

- [ ] Реализовать create/update карточки, варианты/SKU, категории,
  характеристики, изображения, штрихкоды, публикацию, модерацию и архив.
- [ ] Использовать versioned publication snapshot и Product Quality gate из
  [Task 217](217-marketplace-product-publication.md).
- [ ] Выполнять read-after-write и сохранять provider-neutral receipt и drift.

### 176.4 — Цены и остатки

- [ ] Добавить outbound-синхронизацию цены, скидки, НДС и остатков по складам.
- [ ] Учитывать reservation state, batch updates, stale-data guard и bounded
  retry.
- [ ] Не допускать перезаписи более новой версии устаревшим worker result.
- [ ] Сверять локальное и удалённое состояние, включая unknown remote outcome.

### 176.5 — Заказы marketplace

- [ ] Импортировать новые заказы через cursor/checkpoint и inbox/deduplication.
- [ ] Сопоставлять SKU, нормализовать статусы и сохранять immutable order
  snapshot.
- [ ] Поддержать отмену, подтверждение, lifecycle transitions, частичные и
  неизвестные ответы.
- [ ] Связать order с WMS, payment, marking, shipment и settlement через
  typed references, не объединяя их в один статус.

### 176.6 — Fulfillment и отгрузка

- [ ] Закрыть FBS/DBS flow: reserve → pick/pack → shipment creation → label →
  handoff → tracking.
- [ ] Использовать существующие WMS и logistics boundaries, а не прямые
  marketplace-ветки в Core.
- [ ] Проверить crash recovery до и после remote acceptance, lease fencing,
  retry-safe ошибки и manual attention для unknown.

### 176.7 — Возвраты, отмены и компенсации

- [ ] Связать импорт marketplace return с [Task 164](164-returns-cancellations-refunds.md).
- [ ] Поддержать inspection, disposition, reverse stock, refund, reverse
  commission, return logistics и marketplace compensation.
- [ ] Исключить двойной refund, двойной reverse movement и silent rewrite
  исходного заказа/settlement факта.

### 176.8 — Маркировка и УПД

- [ ] Использовать provider-neutral lifecycle из [Epic 171](171-marking-execution-and-upd.md):
  получение кодов, scan, aggregation, circulation и УПД.
- [ ] Связать code/package/document references с order и shipment.
- [ ] Оставить УКЭП, МЧД и ЭДО за изолированными signing/EDO boundaries.

### 176.9 — Реклама и продвижение

- [ ] Использовать [Task 220](220-marketplace-advertising-runtime.md) как
  источник нормализованных advertising facts.
- [ ] Подключить расходы к P&L без двойного учёта.
- [ ] Campaign status, bid, budget и product-link writes допускать только
  после отдельной approval-bound connector qualification.

### 176.10 — Settlement и P&L

- [ ] Связать order, shipment, return, advertising и settlement facts с
  [Task 167](167-channel-unit-economics.md) и [Task 219](219-seller-financial-analytics.md).
- [ ] Показывать выручку, комиссии, логистику, хранение, штрафы, рекламу,
  возвраты, FIFO-себестоимость, payout, ДДС и P&L с детализацией до заказа/SKU.
- [ ] Missing facts, stale attribution и cross-currency gaps оставлять
  видимыми quality findings, не превращать в нули.

### 176.11 — Единый интерфейс marketplace

- [ ] Добавить рабочее место кабинетов, товаров, заказов, отгрузок, возвратов,
  рекламы, settlement, P&L, ошибок и reconciliation.
- [ ] Показывать доступные операции для конкретного account/capability, а не
  статический список кнопок.
- [ ] Для каждого ручного действия отображать policy/approval, риск, причину
  блокировки и безопасный retry/reconcile путь.

### 176.12 — Единая синхронизация и reconciliation

- [ ] Создать operations center с sync status, checkpoints, retries, DLQ,
  drift и unknown remote outcomes.
- [ ] Выделять stale data, missing mappings, duplicate orders, price/stock
  mismatch и marketplace health.
- [ ] Все findings сделать tenant-scoped, append-only и пригодными для
  повторяемого аудита.

### 176.13 — Connector qualification

- [ ] Для каждого provider отдельно подтвердить capabilities, API methods,
  auth scopes, limits, async operations, idempotency, read-after-write и
  normalized errors.
- [ ] Пройти deterministic transport/connector tests, Docker conformance и
  внешний live smoke test в официальном non-production контуре.
- [ ] Не считать live qualification выполненной по manifest или наличию
  credentials; сохранить redacted evidence и дату актуальности.

## Архитектурные ограничения

- Core не ветвится по `ozon`, `wildberries`, `yandex_market` или другим именам.
  Различия остаются в connector capabilities, mappings и runtime support.
- PostgreSQL хранит операционную истину, outbox публикует canonical events,
  inbox/deduplication защищает потребителей. ClickHouse — только
  перестраиваемая аналитическая проекция.
- Order, Offer, Inventory, WMS, Return, Payment, Marking, Settlement и P&L
  остаются отдельными bounded contexts. Marketplace orchestration связывает
  их references/events, но не создаёт дубликаты агрегатов.
- Все mutating routes tenant-scoped, idempotent и policy/approval-gated по
  risk class. Timeout после принятия внешней операции означает `unknown`, а не
  разрешение слепого повтора.
- Деньги — integer minor units + currency; дробные количества — exact
  fixed-point. Timestamps в persistence/events — UTC.
- Raw provider payloads, access tokens, Authorization headers, private signing
  material и лишний PII не сохраняются в обычных API, events, logs или
  evidence.

## Сквозной acceptance gate

Epic считается repository-complete только когда на synthetic WB/Ozon fixture
проходит полный сценарий:

```text
account → product → publication → price/stock → order → reserve
→ pick/pack → shipment → return → settlement → P&L
```

Обязательны проверки:

- duplicate command/event/webhook и повторный worker lease;
- crash до и после remote acceptance;
- timeout, rate limit, async processing и out-of-order status;
- stale price/stock update и missing SKU mapping;
- partial order, partial return, refund/commission reconciliation;
- marking code duplicate/overflow и отказ УПД/МЧД;
- cross-tenant IDs, forbidden capability, expired credential и reauthorization;
- отсутствие token/raw payload в API, logs, events, audit и evidence.

Каждый provider получает отдельный результат `qualified`, `partially_supported`
или `read_only`; зелёный статус одного capability не распространяется на весь
кабинет.

## План выпуска

1. Account state, capability matrix и operations center.
2. WB/Ozon: каталог, цены, остатки и order import.
3. Reserve, WMS, FBS/DBS shipment и tracking.
4. Returns/cancellations, marking/UPD и settlement links.
5. P&L quality, reconciliation, operator UX и failure recovery.
6. Approval-bound writes, advertising controls и Yandex Market/прочие каналы
   только после provider-specific qualification.

До завершения gate внешняя документация и UI обязаны явно говорить
`read_only`/`partially_supported`, если полный сценарий недоступен.

## Verification

Для каждого implementation slice обязательны `gofmt`, `go test ./...`,
`go vet ./...`, `./scripts/check-contracts.sh`,
`./scripts/check-migrations.sh`, `make architecture`, frontend
typecheck/build, Docker conformance и `git diff --check`. Live credentials,
raw marketplace responses и production PII в тесты или репозиторий не попадают.
