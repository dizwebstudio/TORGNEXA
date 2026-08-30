# ADR-0123: AI-помощник оператора как grounded read/preview контур

## Решение

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

## Безопасность и эксплуатация

Raw prompt/provider payload/chain-of-thought/credentials/лишняя PII не
персистируются. FORCE RLS, bounded JSON, optimistic version, idempotency,
монотонная state machine и safe markdown защищают tenant и повторные вызовы.
На небольшом VPS baseline работает без модели; внешний provider включается
только существующей egress/secret policy. Kill switch и retention используют
общие operational контуры.

## Последствия

Первые ответы могут быть `insufficient_data` для источников, не подключённых к
adapter registry. Это намеренное fail-closed поведение. Полная live-provider
и Compose квалификация является deployment evidence, а не основанием выдавать
SDK-only connector за исполненный capability.
