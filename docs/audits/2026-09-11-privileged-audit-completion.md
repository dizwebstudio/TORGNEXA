# Task 234.3 — завершение атомарного аудита — 2026-09-11

Статус: исправлено и проверено в репозитории; deployment не выполнялся.

После ADR-0184 и ADR-0191 оставались manual sync/reconciliation, часть
security settings и OAuth worker refresh. Локальные mutation в этих путях могли
пережить отказ обязательного аудита, а refresh мог вызвать неоткатываемый remote
effect без предварительной durable evidence.

## Закрытая область

| Поверхность | Новая гарантия |
| --- | --- |
| `POST /sync/policies/{id}/run` | Deterministic run + authoritative audit в одной транзакции |
| `POST /connector-accounts:sync` | Все policy runs + один account-sync audit в одной транзакции |
| `POST /reconciliation/jobs` | Deterministic run + audit; exact replay без дубликатов |
| MCP client account create/disable/rotate | Account/receipt/security evidence + Settings audit в одном commit |
| AI provider account create/disable | Secret/account/receipt/evidence/audit в одном commit; replay откатывает неиспользованный candidate secret |
| MCP agent policy / tenant kill switch | Immutable revision + receipt + audit; обязательный `Idempotency-Key` |
| OAuth authorization-code refresh | Deterministic minimized intent до provider call; при отказе evidence remote call не выполняется |

Reconciliation repository теперь присоединяется к существующей
tenant/pool-bound транзакции и различает exact replay и конфликт содержимого.
MCP/AI/agent-governance repositories используют тот же boundary. Неизвестные
ошибки PostgreSQL возвращаются как 500, а 409 остаётся только для явного
version/idempotency conflict.

## Security и privacy

Actor сохраняется через `actor.`-проекцию. Audit summary содержит только
внутренние resource/policy/account IDs, состояние, версию и bounded counts.
Email, raw OIDC subject, credentials, secret reference и provider payload не
попадают в audit/evidence.

Refresh intent имеет стабильный digest для tenant/account/runtime/opaque
reference/current version, но сохраняет только account ID, runtime connector ID
и числовую версию. Поэтому ряд не раскрывает даже opaque secret reference.
Evidence означает допуск попытки, а не успешный remote exchange; последующий
отказ остаётся в нормализованном connector health. Advisory lock и повторное
чтение bundle продолжают гарантировать один refresh/rotation при конкуренции.

## Проверки

- Disposable PostgreSQL из pinned release image, полный migration catalog,
  forced RLS и application role `NOSUPERUSER/NOBYPASSRLS`.
- Failure injection после business statements: manual run, account fan-out,
  MCP/AI account, MCP policy/kill switch и refresh intent.
- Успешный retry и exact replay: одна business row, один receipt и один audit;
  AI replay также оставляет одну secret reference.
- Повтор `Idempotency-Key` с другим телом policy/kill-switch возвращает 409 и
  не добавляет business или audit rows.
- Refresh failure запрещает provider call; после восстановления evidence retry
  создаёт одну строку и одну ciphertext rotation. One-slot и bounded-pool
  concurrency regressions проходят.
- PostgreSQL gate: 39 top-level tests и 100 PASS с подслучаями под `-race`.
- Полный `go test ./...`: 215 пакетов с тестами; `go vet ./...`, contracts,
  architecture, migrations, generated SDK и TypeScript declarations — PASS.

[Команды и журналы](2026-09-11-evidence/privileged-audit-validation.md).

## Применение

Миграция не нужна: используются существующие `operation_receipts`,
`audit_records` и `security_evidence`. Обновить API и worker вместе из-за
расширения refresh coordinator contract. Клиенты MCP policy installation и
kill-switch mutation должны передавать `Idempotency-Key`; OpenAPI и SDK
перегенерированы.

Исторически пропущенные записи автоматически не восстанавливаются. Общая Task
234 остаётся `in_progress` из-за других подпунктов, но release blocker 234.3
закрыт.

[Решение ADR-0193](../../adr/0193-atomic-privileged-dispatch-and-refresh-intent.md).
