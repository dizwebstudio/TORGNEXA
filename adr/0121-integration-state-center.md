# ADR 0121: Единый центр состояния интеграций

## Status

Accepted for Task 168.

## Context

Состояние кабинета, runtime admission, разрешённые операции, проверка
подключения, синхронизация, сверка и webhook evidence живут в разных
источниках. Сводить их в один зелёный badge опасно: health-only/SDK-only
коннектор не становится commerce runtime, а устаревший healthy результат не
может авторизовать запись.

## Decision

1. Ввести provider-neutral read model с десятью независимыми измерениями:
   runtime, account, credential, configuration, health, capability, sync,
   reconciliation, webhook и rate limit. Источники остаются authoritative.
2. Вычислять `overall` чистым детерминированным reducer с fail-closed
   приоритетом: unsupported, blocked, setup, reauthorization, disabled,
   degraded, stale, attention, syncing, healthy, unknown. Вторичные issues
   всегда сохраняются рядом с dominant issue.
3. Каждое измерение содержит только bounded evidence reference: source kind/ref,
   timestamps, TTL/age, reason code, visibility и digest. Secrets, raw provider
   errors, OAuth material и PII не копируются в snapshot, events или UI.
4. Добавить tenant-scoped immutable snapshot/transition/action metadata и
   coalescing recompute queue в PostgreSQL с FORCE RLS. Эти таблицы пересоздаются
   из account/capability/config/sync/reconciliation источников.
5. GET `/api/v1/integration-center` и detail route выполняют только scoped
   database reads; remote check, OAuth, sync, retry и approval остаются у
   существующих owners. Kafka/EventBus получает только metadata transitions,
   worker coalesces recompute work, SSE отправляет invalidation signal.

## Alternatives considered

- Считать каталог интеграций единственным статусом: отклонено, потому что он
  не знает account grants, stale evidence и worker/reconciliation.
- Хранить текущий статус вместо source references: отклонено, это создаёт
  второй источник истины и ломает rebuild после сбоя.
- Проверять провайдера на каждом GET: отклонено из-за latency, rate limits,
  непредсказуемого egress и невозможности безопасно кэшировать ответ.
- Передать reducer во frontend: отклонено, permissions и fail-closed правила
  должны быть одинаковыми для API, worker и UI.

## Compatibility impact

Контракт additive: OpenAPI получает два GET route и versioned schemas,
generated SDK regenerates без изменения существующих методов. Добавлены два
canonical event payloads и topic `commerce.integration.events.v1`.

## Migration and data impact

Migration `000035_integration_state_center.sql` expand-only. Она добавляет
bounded snapshots, account projections, immutable transitions/action receipts
и coalescing queue с composite organization/workspace keys, indexes и FORCE
RLS. Источники commerce/integration не переписываются; evidence retention и
legal hold применяются отдельно от rebuildable projection.

## Security and privacy impact

Tenant scope берётся только из authenticated context. API не принимает
organization/workspace selectors и не читает secret bytes. Field visibility
может быть `partial`/`redacted`, а неизвестное состояние не преобразуется в
healthy. Все операторские actions несут permission, risk, expected version и
idempotency metadata; центр сам не исполняет произвольный HTTP/SQL/shell.

## Operational impact

Снимки bounded (100 rows/page, 32 issues, 16 actions), queue coalesces by
tenant/account, consumer retries через общий Kafka policy, а source outage
даёт partial/stale evidence. Projection можно отключить и восстановить из
authoritative таблиц без остановки account writes. Метрики и runbook описаны
в `docs/operations/168-integration-state-center.md`.

## Consequences

Оператор видит одну согласованную картину и безопасное следующее действие,
при этом отдельные контуры AI/Finance/Delivery/CRM не выдаются за generic
commerce sync. Появляется дополнительная миграция и recompute worker, а
qualifying runtime/capability evidence по-прежнему требуется перед любой
операцией записи.
