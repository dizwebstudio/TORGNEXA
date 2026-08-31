# ADR-0123: AI-помощник оператора как grounded read/preview контур

Status: Accepted

## Context

Оператору нужен безопасный способ получить объяснение текущего состояния
магазина без превращения модели в источник истины или привилегированный
исполнитель.

## Decision

Добавить provider-neutral модуль `internal/core/operatorassistant` и
tenant-scoped PostgreSQL read model для сессий/runs/evidence metadata. Сервер
определяет intent, источники, freshness, лимиты и риск. AI-провайдер получает
только redacted bounded context, маркированный `UNTRUSTED_TOOL_DATA`.

Ответы могут содержать факты, рекомендации и typed action preview. Любая
мутация остаётся в каноническом доменном API и, если требуется, в Task-017
approval/outbox/inbox контуре; preview endpoint не исполняет действие.

## Почему

Существующий `settings/ai-providers:analyze` совместим для legacy Ask AI, но
caller-assembled prompt не обеспечивает citations, freshness, actor scope,
run lifecycle и policy boundary. Новый путь отделён от legacy API и не
создаёт второго источника истины.

## Security and privacy impact

Raw prompt/provider payload/chain-of-thought/credentials/лишняя PII не
персистируются. FORCE RLS, bounded JSON, optimistic version, idempotency,
монотонная state machine и safe markdown защищают tenant и повторные вызовы.
На небольшом VPS baseline работает без модели; внешний provider включается
только существующей egress/secret policy. Kill switch и retention используют
общие operational контуры.

## Migration and data impact

Миграция 000038 добавляет только новые tenant-scoped таблицы; существующие
доменные записи не переписываются. Таблицы можно отключить и пересобрать без
остановки commerce writes.

## Operational impact

Детерминированный baseline не требует AI-провайдера. Состояния run монотонны,
lease/retry ограничены, а source outage виден оператору как partial/stale.

## Alternatives considered

- Оставить только legacy completion endpoint: отклонено из-за отсутствия
  server-side grounding, citations и lifecycle.
- Дать модели прямую запись: отклонено, потому что это обходит policy,
  approval, idempotency и audit.

## Compatibility impact

Legacy `/settings/ai-providers:analyze` остаётся совместимым; новые пути
`/assistant/*` добавляются аддитивно и используют отдельные permission.

## Consequences

Первые ответы могут быть `insufficient_data` для источников, не подключённых к
adapter registry. Это намеренное fail-closed поведение. Полная live-provider
и Compose квалификация является deployment evidence, а не основанием выдавать
SDK-only connector за исполненный capability.
