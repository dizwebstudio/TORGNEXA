# ADR-0173: Marketplace Operations v1

Status: Accepted

## Context

TORGNEXA уже содержит отдельные marketplace surfaces: publication, inventory,
settlement, financial analytics, advertising и WMS foundations. Но наличие
нескольких read/capability endpoints не доказывает готовность пользовательского
marketplace-продукта. Для оператора важен полный контур от подключения кабинета
до заказа, отгрузки, возврата и P&L.

Пользовательский Epic 176 объединяет эти этапы. Его нельзя реализовать второй
моделью заказа или отдельной marketplace-базой: это породило бы расхождение с
каноническими Product/Offer, Order, Inventory/WMS, Return, Payment, Marking,
Settlement и Financial bounded contexts.

## Decision

Зафиксировать Epic 176 как provider-neutral integration and release gate.
Репозиторный task key — `223`, потому что числовые ключи `176–222` уже заняты;
пользовательский номер Epic не меняется.

Marketplace operations связываются через существующие доменные агрегаты,
typed connector capabilities, Transactional Outbox, Inbox/deduplication,
approval/policy и reconciliation. Новый orchestration слой может хранить
только tenant-scoped operation references, checkpoints, status projections,
findings и versioned evidence; он не становится источником истины для
товаров, остатков, денег или заказов.

Для каждого account runtime вычисляет truthful support state по capabilities:

```text
read_only | partially_supported | qualified
```

`qualified` разрешается только после deterministic tests, Docker conformance и
отдельного provider live smoke/evidence. Manifest, SDK declaration, health
check или наличие credentials не являются доказательством полной поддержки.

Полный DoD — сценарий `account → product → publication → price/stock → order →
reserve → pick/pack → shipment → return → settlement → P&L` на синтетических
fixture с проверками duplicate/replay, crash до/после remote acceptance,
timeout/unknown, stale data, partial quantities, cross-tenant IDs и отсутствия
секретов в durable state. Неподтверждённые операции остаются fail-closed.

## Alternatives considered

1. Объявить WB/Ozon полноценными marketplace-коннекторами по наличию
   `products.read` и `inventory.read` — отклонено: read surface не доказывает
   order, fulfillment, return и settlement lifecycle.
2. Создать единый marketplace aggregate, который копирует order, stock и
   finance — отклонено: он станет второй операционной истиной и нарушит
   существующие bounded contexts.
3. Спрятать неподдержанные операции за универсальными кнопками или SDK-типы —
   отклонено: UI и runtime должны быть capability-aware и default-deny.
4. Включить все remote writes сразу — отклонено: writes требуют approval,
   idempotency, read-after-write, rate-limit budget и unknown-result
   qualification отдельно для каждого провайдера.

## Consequences

Положительный эффект — пользователь видит честный статус кабинета и единый
операционный путь, а release decision опирается на воспроизводимое evidence.
Существующие задачи 164, 167, 171, 217–220 остаются источниками своих
доменов; Epic 176 добавляет координацию и сквозной gate, а не дублирование
логики.

Цена решения — полная готовность наступает позже, чем готовность отдельного
read endpoint. До qualification WB/Ozon и другие channels обязаны оставаться
`read_only` или `partially_supported`, даже если часть операций уже работает.

## Compatibility impact

Архитектурное решение не меняет существующие публичные API, event versions или
connector capability names само по себе. Новые API/events, если они появятся
в дочерних implementation tasks, должны быть additive, описаны в OpenAPI и
canonical event envelope и пройти generated SDK/runtime parity.

Existing product publication, financial, advertising, returns and marking
contracts remain valid. Any breaking change requires a separate ADR and a new
versioned contract.

## Migration and data impact

Epic 176 не требует отдельной миграции только для фиксации release gate.
Каждая дочерняя доменная задача отвечает за свою expand-only migration,
catalog checksum, backup gate, forced RLS и rollback через capability
disablement/worker drain. Нельзя делать destructive down migration или молча
переписывать исторические order, inventory, settlement и P&L facts.

## Security and privacy impact

Credentials выдаются только через scoped SecretProvider. Access tokens,
Authorization headers, raw provider payloads, Data Matrix values, private
signing material, payment credentials и лишний PII не входят в operation state,
events, logs, audit metadata, evidence или обычные API responses.

Все команды получают organization/workspace context из authenticated request,
проверяют capability и risk policy. AI, MCP, n8n и operator UI не могут
обойти approval или включить capability.

## Operational impact

Operations center должен показывать freshness, checkpoints, retries, DLQ,
unknown outcomes, missing mappings, stale data и reconciliation drift.
Внешний вызов всегда bounded by timeout/rate limit и имеет normalized error.
После timeout worker не повторяет потенциально принятую запись вслепую:
сначала выполняется status read/reconciliation или создаётся manual attention.

Rollback дочернего slice — отключение capability, остановка новых claims и
drain worker; уже подтверждённые append-only facts не удаляются.
