# Исправления webhook A03–A04

Изменения в коде по [Task 234.2](../../tasks/issues/234-security-correctness-performance-hardening.md)
и [ADR-0185](../../adr/0185-verified-webhook-atomic-commit-and-retry.md).
Рабочие контейнеры и настройки внешних провайдеров не обновлялись.

## Результат

- Payment receipt, изменение статуса, обязательный audit и Outbox коммитятся
  одной транзакцией. Ошибка любой стадии оставляет доставку повторяемой.
- После независимой проверки подлинности сбой сохранения payment/commerce/social
  webhook даёт `503`, `{}`, `Retry-After: 5`, `Cache-Control: no-store`.
  Успешный provider-specific ack выдаётся только после коммита.
- Дубликат успешно применённой доставки не повторяет payment/audit/outbox.
  Совпадение delivery ID с другими проверенными данными отклоняется.
- Commerce replay использует первое сохранённое время приёма, если provider
  payload не даёт собственного timestamp. Проверка остальных полей остаётся
  строгой; обычный EventBus Inbox не меняет fingerprint semantics.
- Runtime config принимает существующее поле `webhook_secret_reference` только
  как валидную непрозрачную ссылку SecretProvider. Plaintext и вложенные
  sensitive keys по-прежнему запрещены. Это разблокирует конфигурацию social
  webhook без сохранения самого секрета в JSON.
- OpenAPI и generated SDK обновлены; схемы БД и событий не изменены.

## Интеграционные проверки

`./scripts/check-audit-postgres.sh` создаёт PostgreSQL без сети, применяет полный
migration catalog, использует forced RLS и непривилегированную application role,
запускает сценарии с `-race` и удаляет контейнер.

- Payment: отказ INSERT receipt, UPDATE payment, INSERT audit, INSERT outbox,
  deferred COMMIT; после ошибки нет receipt или частичных эффектов, retry
  успешно завершает доставку ровно один раз.
- Реальный optimistic conflict: блокировка строки другим PostgreSQL transaction,
  конкурентное изменение версии, rollback receipt и успешная redelivery.
- Одинаковые и различные concurrent delivery IDs; replay без повторного audit.
- Потеря DB connection после verification; восстановление соединения и replay.
- WooCommerce, Telegram и MAX: настоящие admitted verifiers с синтетическими
  encrypted secrets; неверная подпись/secret остаётся uniform 200, verified
  outbox failure даёт 503, повтор создаёт ровно один Inbox и один Outbox.
- Commerce без timestamp: изменившееся время доставки не вызывает collision;
  изменённый canonical payload при прежнем delivery ID всё ещё отклоняется.
- Ранее добавленные PostgreSQL-сценарии A05–A08 проходят в том же запуске.

## Границы и обновление

Одинаковые pre-verification ответы сохраняются, включая сбои account/config/secret
lookup или remote verification: без доказанной подлинности новый verified-only
ответ не выбирается. Для них и исчерпания retry у провайдера нужна reconciliation.

При обновлении API/worker выполните сверку интервала работы старой версии:
существующий старый payment receipt не доказывает успешное применение статуса.
Старые commerce receipts с потерянной наносекундной точностью времени также
могут требовать сверки. Append-only история автоматически не переписывается.
Инструкции настройки обязательного `subscription` остаются в
[предыдущем отчёте](2026-09-09-a05-a08-fixes.md#подключение-webhook-после-обновления).

Отдельный receipt worker не требуется для новой атомарной операции: rollback
возвращает доставку провайдеру через 503. Существующий reconciliation worker
остаётся страховкой. Другие открытые пункты аудита этим изменением не закрыты.

## Результаты проверки

- `go test ./...`: PASS, 215 пакетов с тестами; `go vet ./...`: PASS.
- PostgreSQL `-race`: PASS, 18 верхнеуровневых сценариев A03–A08
  (40 с подслучаями, в том числе 9 новых верхнеуровневых сценариев webhook).
- Contracts, architecture, generated SDK и TypeScript declarations: PASS.
- `gofmt` и `git diff --check`: PASS; SQL-миграции не менялись.

[Журналы и окружение](2026-09-09-evidence/webhook-validation.md).
