# ADR-0177: Единая глубина интеграций и readiness-коннекторов

Status: Accepted

## Context

В каталоге TORGNEXA находятся 61 манифест, но наличие манифеста, SDK или
успешного health-check не доказывает наличие бизнес-операции. Без общей
модели Integration Center и release-report показывают слишком оптимистичную
картину, а оператор не видит разницу между только подключением, чтением,
частичной записью и подтверждённым runtime.

Текущий authoritative runtime-каталог содержит 16 явных `HealthOnly` записей
и две zero-operation specialized surface записи. Число «17 health-only» в
черновике Task 226 больше текущего источника и не должно воспроизводиться
искусственно.

## Decision

Ввести provider-neutral readiness matrix, генерируемую из manifest, reviewed
runtime-support catalog и redacted conformance reports. Матрица содержит
профиль connector-а и отдельные capability entries с direction, scope
metadata, risk class, idempotency, read-after-write, webhook/reconciliation,
rate limits, owner, priority, evidence age, blocker и next action.

Статусы профиля:

`manifest_only → health_only → read_only → partially_supported → ready →
qualified`, а также `degraded`, `reauthorization_required` и
`not_available`. `ready` означает, что repository runtime surface и
детерминированный conformance admission присутствуют. `qualified` разрешён
только при retained credentialed sandbox/live evidence exact capability;
одного ping для этого недостаточно.

Матрица доступна через `GET /api/v1/connector-readiness` и detail route, в
generated SDK, MCP read-only tool и Integration Center. Эти поверхности
показывают весь каталог, включая ещё не настроенные и специализированные
коннекторы. Запись из этой витрины невозможна. Worker и domain API по-прежнему
используют capability, approval, account health, SecretProvider, outbox/inbox,
lease и reconciliation gates; readiness не создаёт обход этих проверок.

## Consequences

Оператор получает честную картину по всем 61 connector-ам и может отличить
локальную runtime-готовность от внешней qualification. Release gate
воспроизводимо проверяет ID inventory, обязательные redacted reports,
несекретность matrix и отсутствие qualified без live evidence. Изменение
операционной поверхности требует повторной генерации matrix и conformance.

Система не хранит credentials, raw provider responses или PII в matrix,
events, audit или UI. Readiness snapshot является checked-in contract/evidence;
account-specific health, sync and reconciliation остаются в существующих
tenant-scoped durable repositories.

## Compatibility impact

Изменение аддитивное: новый read-only endpoint, generated SDK методы и MCP
инструмент не меняют существующие account, sync, event или write-контракты.
Технические status codes стабильны, а новые поля не раскрывают credentials.

## Migration and data impact

Новая SQL-миграция не требуется. Матрица является versioned checked-in
contract, а состояние кабинета, sync checkpoint, outbox/inbox и reconciliation
продолжают храниться в существующих tenant-scoped таблицах.

## Security and privacy impact

Readiness routes требуют authenticated tenant scope и permission
`integrations.center.read`; MCP проходит agent governance. SecretProvider,
approval, policy, kill switch и account health остаются обязательными для
удалённых действий. Matrix и reports содержат только redacted metadata.

## Operational impact

Поддержка использует owner, priority, evidence age, blocker и next action из
одной матрицы. При отзыве доступа оператор действует через reauthorization;
при timeout разбирает `unknown` через reconciliation, не выполняя слепной
повтор. Static gate запускается в CI до release qualification.

## Alternatives considered

Определять готовность по manifest capabilities или ping отклонено: это
создаёт ложные зелёные статусы. Создать отдельный provider-specific registry
отклонено: runtime остаётся capability-driven. Подменить отсутствие
credentialed evidence статусом `qualified` отклонено из-за риска неявных
remote writes.

## Operational and release boundary

Первая qualification wave — core commerce и logistics, затем finance,
identity/notifications, AI/social и specialized surfaces. Каждый новый
`qualified` требует exact connector/capability evidence, scopes,
read-after-write, timeout/unknown, retry, reconciliation и Docker conformance.
До этого операции отображаются как `ready`, `read_only`, `partially_supported`
или `health_only` с blocker и next action.
