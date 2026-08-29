# ADR-0116: Базовый конструктор автоматизаций

## Статус

Принято для Task 163, версия v1.

## Решение

Автоматизация хранится как tenant-scoped декларативный DAG в PostgreSQL. Draft
валидируется и детерминированно компилируется в план с SHA-256 digest. Publish
создаёт immutable версию; исполнение создаёт отдельные `workflow_runs`,
`workflow_step_runs` и append-only evidence. Kafka/EventBus используется только
как durable transport триггеров, а time triggers и leases принадлежат PostgreSQL.

Первый action catalog ограничен typed application ports:

| Action | Risk | Capability | Retry | Dry-run |
| --- | --- | --- | --- | --- |
| `notification.create` | `write_safe` | `notifications.create` | да | да |
| `reconciliation.run` | `write_safe` | `reconciliation.run` | да | да |
| `approval.request` | `write_sensitive` | `approval.request` | нет | да |
| `sync.dry_run` | `read` | `sync.preview` | да | да |

Ссылки в definition ограничены безопасными typed fields и opaque references.
Секреты, raw payload, arbitrary HTTP/SQL/shell/code, browser automation и
provider-specific branches запрещены. Чувствительные actions проходят обычный
Task-017 policy/approval gate.

## Последствия

- Нельзя объявить action исполняемым только по manifest capability: нужен
  зарегистрированный host adapter.
- Retrying безопасен только для retry-safe action и использует idempotency key.
- UI обязан показывать реальный каталог действий, риск и лимиты, а не обещать
  поддержку не подключённых портов.
- На малой VPS fan-out, размер definition, число узлов и длительность запуска
  ограничены server-side.
