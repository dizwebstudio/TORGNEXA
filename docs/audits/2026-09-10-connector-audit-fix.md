# Connector mutations and authoritative audit — 2026-09-10

Статус: исправлено и проверено в репозитории; deployment не выполнялся.

При отказе аудита прежний код уже успевал создать секрет, сменить привязку
аккаунта и отозвать предыдущий секрет. PostgreSQL regression до исправления
завершался `credential binding escaped audit rollback`. Теперь эти изменения
и audit record фиксируются одной транзакцией; API сообщает успех после COMMIT.

## Область исправления

| Операция | Единица атомарной фиксации |
| --- | --- |
| Создание аккаунта | Account + audit; одинаковый replay без второго audit |
| Credentials | Ciphertext/reference + binding/version/status + revoke old + audit |
| Enable/disable | Account status/version + audit |
| Capabilities | Snapshot/history + account version + audit |
| Health check | Normalized health/history + account version + audit |
| OAuth start | Encrypted PKCE + pending state + audit; replay без orphan secret |
| OAuth claim | Consume state + revoke PKCE + `oauth_callback_claimed` audit |
| OAuth completion | Encrypted bundle + binding + revoke old + `oauth_completed` |
| OAuth exchange failure | Normalized health/history + `oauth_failed` audit |
| Bootstrap preview | Preview + audit; replay без второго audit |
| Initial import | Consume preview + durable job + audit; replay без второго audit |
| Schedule | Schedule version/state + audit |

Репозитории аккаунтов и синхронизации присоединяются к существующей транзакции
по context. Смена tenant или database pool запрещена. Один application connection
достаточен; вложенный COMMIT не выполняется. Провайдер секретов обязан поддерживать
`TransactionalProvider`, иначе mutation не запускается.

Дополнительно исправлены обнаруженные интеграцией препятствия успешному пути:
удалён запрещённый PostgreSQL audit key `has_secret_reference`; timestamps
preview/job/schedule приводятся к UTC перед проверкой доменных инвариантов.
Audit actor использует существующую псевдонимную проекцию `actor.`; summary не
получает raw subject, secret reference, OAuth state/code, credentials или bodies.

## OAuth и повторы

Remote exchange запускается после COMMIT claim + audit. При отказе claim audit
провайдер не вызывается, state и PKCE остаются доступны для повтора. После
успешного claim state не открывается заново: даже если последующее сохранение
bundle/audit завершилось ошибкой, повтор callback даёт 409 и не вызывает
провайдера ещё раз. Требуется новый OAuth start с новым idempotency key.

Ошибка completion сохраняет прежнюю локальную привязку, но PostgreSQL не может
отменить обмен кода у провайдера. Health probes выполняются до локальной
транзакции; runtime refresh сохраняет отдельную границу ADR-0104.

## Проверки

- Реальная PostgreSQL 18.6: полный каталог миграций, forced RLS, роль
  NOSUPERUSER/NOBYPASSRLS, отдельный disposable контейнер без сети.
- 19 новых конечных сценариев: audit/revoke/COMMIT/cancellation rollback,
  успешный retry, stale replay, два конкурентных credential writes,
  account/history/bootstrap rollback, cross-tenant/pool rejection, OAuth
  start/claim/completion/provider failures и отсутствие повторного exchange.
- Общий PostgreSQL gate с прежними A02–A09/SSE regressions:
  33 теста верхнего уровня, 94 PASS с подслучаями, `-count=1 -race`.
- Targeted race/unit, `go test ./...` (215 пакетов с тестами), `go vet ./...`,
  contracts, architecture, generated SDK и TypeScript declarations,
  frontend tests/build — PASS.
- Исправлен отдельный CI blocker: четыре first-party GHCR runtime repositories
  добавлены в явный allowlist. Digest pinning и отказ неизвестным repositories
  сохранены. Полный `make policy` — PASS.

[Команды и журналы](2026-09-10-evidence/connector-audit-validation.md).

## Применение и оставшаяся область

Обновить API instances. Миграции, новые persisted поля, public event или SDK
payload изменения не нужны; OpenAPI/SDK hashes обновлены. Рабочие базы и
контейнеры не изменялись. Исторически пропущенные audit records автоматически
не восстанавливаются.

Закрыт выявленный дефект account/bootstrap writes. Runtime config уже защищён
ADR-0184. Manual sync/reconciliation dispatch, worker refresh и остальные
security settings остаются отдельным inventory Task 234.3. Общая Task 234
сохраняет `in_progress`; это исправление не является новым аудитом всего проекта.

[Решение ADR-0191](../../adr/0191-atomic-connector-account-audit.md).
