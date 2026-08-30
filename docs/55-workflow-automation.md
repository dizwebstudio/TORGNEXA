# Автоматизации

Раздел «Автоматизации» позволяет собрать проверяемую цепочку из события или
расписания, условий и allowlisted actions. В production runtime доступна только
декларативная модель: workflow не выполняет пользовательский код и не получает
прямой доступ к SQL, HTTP, секретам или connector packages.

## Жизненный цикл

`draft -> published -> paused -> archived`.

Публикация создаёт новую immutable версию и детерминированный plan digest.
Запуск имеет собственную idempotency key и проходит состояния `queued`,
`running`, `waiting_approval`, `waiting_retry`, `completed`, `failed` или
`cancelled`. Исторические step evidence не переписываются.

## Доступные действия v1

- `notification.create` — создать уведомление в tenant inbox;
- `reconciliation.run` — поставить безопасный reconciliation request;
- `approval.request` — создать обычный запрос Task-017 для sensitive action;
- `sync.dry_run` — выполнить metadata-only preview.

Для каждого действия проверяются capability, policy, idempotency, timeout и
retry classification. Отсутствующий typed host adapter означает отказ до
любого внешнего вызова.

### Типизированная конфигурация действий

| Действие | Разрешённые поля конфигурации | Поведение |
|---|---|---|
| `notification.create` | `recipient_id`, `dedupe_key`, `severity`, `title`, `body`, `category` | Запись в tenant inbox; ключ по умолчанию детерминирован от run/node. |
| `reconciliation.run` | `policy_id`, `mode`, `trigger_ref` | Создаёт idempotent Task‑014 run; транспорт выполняет штатный reconciliation worker. |
| `approval.request` | `action`, `resource_type`, `resource_id`, `risk` | Создаёт Task‑017 request; до решения side effect не выполняется. |
| `sync.dry_run` | `mode` | Metadata-only preview без удалённого write. |

Условие принимает только булево поле `result`, `value` или `enabled`; delay —
только целое `seconds` от 0 до 86 400. Произвольные ключи, выражения, shell,
SQL, HTTP и поля, похожие на secret/token/password, отклоняются компилятором.
Retryable action получает максимум восемь попыток; затем run становится
`failed` с `retry_exhausted` и требует операторского retry с новым run id.

## REST API

- `GET/POST /api/v1/workflows` — список и создание draft;
- `POST /api/v1/workflow-commands/validate` — проверка и plan digest без сохранения;
- `POST /api/v1/workflow-commands/publish` — optimistic publish новой версии;
- `POST /api/v1/workflow-commands/pause` и `/archive` — остановка или архивирование;
- `POST /api/v1/workflow-commands/run` — ручной idempotent test-run;
- `GET /api/v1/workflow-runs` и `/workflow-runs/{id}` — наблюдение за runs;
- `GET /api/v1/workflow-runs/{id}/steps` и `/evidence` — bounded timeline и
  неизменяемые свидетельства шага (только статусы, digest и machine codes).
- `POST /api/v1/workflow-run-commands/retry` — replay failed/cancelled run с новым run id;
- `POST /api/v1/workflow-run-commands/cancel` — fenced cancel по optimistic version.

Все mutation endpoints требуют `Idempotency-Key`; tenant/workspace берутся из
аутентифицированной сессии. Payload хранится только в bounded JSON definition,
input digest и opaque references; credentials и raw event body не сохраняются.

Ограничения runtime: не более 100 активных workflow на workspace, 120 новых
run за минуту и 8 одновременно исполняемых run. При превышении API возвращает
`429`, а worker оставляет исходную очередь и evidence без потери. Scheduler
держит состояние расписания в PostgreSQL; Kafka используется только для
доставки событий, а повторная доставка дедуплицируется по `event_id`.
Обработчик событий сначала фиксирует run и Inbox receipt в одной PostgreSQL
транзакции. Поэтому crash до commit не оставляет «осиротевший» run, а повтор
того же `event_id` не создаёт второй logical run. Worker также может забрать
просроченный lease со статусом `running`: завершённые шаги будут пропущены, а
незавершённый typed adapter повторится с тем же idempotency boundary.

## Операционная проверка

После миграции 000027 выполните:

```bash
make community-up
make test
make workflow-qualification
```

`make workflow-qualification` сохраняет machine-readable evidence в
`qualification/evidence/workflow-<UTC>/` и выполняет Go/contract/frontend
проверки в disposable Docker-контейнере. Это repository qualification, а не
подмена live-provider проверки: перед production нужно дополнительно выполнить
`make production-qualification` на целевой Compose-топологии и сохранить
нагрузочный/chaos evidence (Kafka/PostgreSQL restart, redelivery, rate limit и
approval expiry).

Проверка вручную: открыть «Автоматизации», создать draft с узлами
`condition -> notification.create`, нажать «Проверить», затем «Опубликовать» и
«Тестовый запуск». В списке runs должен появиться `queued`; worker исполняет
только зарегистрированные adapters, а approval action остаётся в
`waiting_approval` до решения Task-017.

Нажатие на идентификатор запуска открывает «Хронологию запуска»: отдельные
таблицы шагов и свидетельств загружаются из `/steps` и `/evidence`. В них не
показываются входные события, тексты внешних ответов, токены или другие
секретные поля.

Для восстановления: сначала поставьте workflow на паузу, проверьте timeline и
machine error code, затем используйте retry (создаётся новая run identity) или
cancel. Исторические `workflow_step_evidence` и approval/audit записи не
изменяются и не удаляются операционными командами.
