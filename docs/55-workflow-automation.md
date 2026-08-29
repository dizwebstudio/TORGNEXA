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

## REST API

- `GET/POST /api/v1/workflows` — список и создание draft;
- `POST /api/v1/workflows:validate` — проверка и plan digest без сохранения;
- `POST /api/v1/workflows:publish` — optimistic publish новой версии;
- `POST /api/v1/workflows:pause` и `:archive` — остановка или архивирование;
- `POST /api/v1/workflows:run` — ручной idempotent test-run;
- `GET /api/v1/workflow-runs` и `/workflow-runs/{id}` — наблюдение за runs.

Все mutation endpoints требуют `Idempotency-Key`; tenant/workspace берутся из
аутентифицированной сессии. Payload хранится только в bounded JSON definition,
input digest и opaque references; credentials и raw event body не сохраняются.

## Операционная проверка

После миграции 000027 выполните:

```bash
make backend-migrate
make test
```

Проверка вручную: открыть «Автоматизации», создать draft с узлами
`condition -> notification.create`, нажать «Проверить», затем «Опубликовать» и
«Тестовый запуск». В списке runs должен появиться `queued`; worker исполняет
только зарегистрированные adapters, а approval action остаётся в
`waiting_approval` до решения Task-017.
