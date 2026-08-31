# AI-помощник оператора (Task 169)

Помощник — это tenant/actor-scoped read-модуль над каноническими источниками
TORGNEXA. Он возвращает факты, состояние подтверждения и ссылки на evidence;
модель не является источником истины и не получает права оператора.

## Быстрый запуск в Compose

1. Примените миграции обычным entrypoint проекта (`make backend-migrate`).
2. Запустите API, worker и PostgreSQL/Kafka профилем небольшой VPS:
   `docker compose --profile community up -d api worker postgres kafka`.
3. Войдите через штатный OIDC. Отдельно настраивать AI-провайдера не нужно:
   baseline отвечает детерминированно по snapshot интеграций. Провайдеры ИИ
   остаются только транспортом рекомендаций и проходят существующую egress
   policy.
4. Откройте раздел **AI-помощник** или `/assistant`.

Для повторяемой проверки API используйте synthetic smoke (требуются только
`TORGNEXA_URL` и короткоживущий `ACCESS_TOKEN`):

```bash
TORGNEXA_URL=http://127.0.0.1:8080 ACCESS_TOKEN="$ACCESS_TOKEN" \
  ./scripts/operator-assistant-smoke.sh
```

Скрипт создаёт одну actor-scoped сессию, задаёт пять вопросов первого среза,
проверяет `state`/`grounding_state` и завершает ошибкой при credential-shaped
строках. Тела ответов не печатаются и реальные данные в fixtures не нужны.

## API

Все пути ниже имеют префикс `/api/v1`, требуют OIDC, tenant scope и permission.
Каждый POST принимает `Idempotency-Key`.

| Метод | Путь | Назначение |
|---|---|---|
| POST/GET | `/assistant/sessions` | создать или перечислить actor-scoped сессии |
| GET | `/assistant/sessions/{session_id}` | проверить сессию |
| POST | `/assistant/sessions/{session_id}/messages` | классифицировать вопрос, собрать контекст, создать run и вернуть ответ |
| GET | `/assistant/runs/{run_id}` | получить нормализованный run |
| POST | `/assistant/runs/{run_id}:cancel` | отменить незавершённый run с optimistic version |
| POST | `/assistant/feedback` | записать bounded feedback по своему run |
| POST | `/assistant/action-previews/{preview_id}:approve` | создаёт Task-017 approval request; прямого исполнения нет |

Пример первого запроса:

```bash
curl -X POST "$TORGNEXA_URL/api/v1/assistant/sessions" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Idempotency-Key: assistant-session-$(uuidgen)" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Операторский помощник","locale":"ru-RU"}'
```

Затем отправьте один из поддержанных вопросов: «Что требует внимания в
интеграциях?», «Почему товар не публикуется?», «Какие каналы просели?», «Что
будет с остатком и когда пополнять?», «Сформируй план исправления».

## Grounding и безопасность

- `grounding_state=grounded` допускается только при evidence; иначе ответ
  помечается `partially_grounded`, `insufficient_data`, `stale_data`,
  `source_unavailable` или `refused`.
- Источник передаёт только digest, watermark, возраст/TTL, visibility и
  разрешённый deep link. Raw provider payload, prompt, chain-of-thought,
  токены, ключи и лишние PII не сохраняются.
- Текст товара, отзыв, webhook и ответ провайдера — `UNTRUSTED_TOOL_DATA`.
  Инструкции «игнорируй правила», SQL/shell/HTTP и запросы секретов
  отклоняются до retrieval.
- `action_previews` — типизированный allowlist без side effect. Sensitive
  write требует существующий approval/policy/capability/version/idempotency;
  endpoint preview не выполняет доменную операцию.

## Проверка и диагностика

```bash
docker compose ps
docker compose logs --tail=100 api worker
docker compose exec api sh -lc 'wget -qO- http://127.0.0.1:8080/api/v1/health'
```

Если источник интеграций недоступен, ожидайте `source_unavailable` или
`insufficient_data`, а не зелёное утверждение. Если permission отсутствует,
UI скрывает раздел, а API возвращает 403. Повтор с тем же idempotency key не
создаёт второй run. Отмена использует `expected_version`; конфликт версии
возвращает 409. Состояния worker монотонны и не возвращаются из terminal в
queued.

Очередь `operator_assistant` добавляется миграцией `000043_operator_assistant_runtime.sql`.
Она использует тот же PostgreSQL lease/SKIP LOCKED boundary, что и остальные
runtime jobs. При отсутствии provider queued-run переводится в
`provider_unavailable` с кодом `provider_not_configured`; это честное состояние,
а не сгенерированный ответ. Повторный запуск smoke после настройки provider
не создаёт второй run благодаря `Idempotency-Key`.

Action preview сначала сохраняется как pending. Для `sensitive_write` кнопка
создаёт governed approval request (`approval.request` / `assistant_action_preview`),
записывает audit и переводит preview в `approved`; доменная операция должна
быть выполнена только владельцем домена после повторной проверки policy,
capability и версии. Preview с истёкшим сроком или повторным кликом получает
конфликт и не вызывает connector.

## Retention и production admission

Таблицы `assistant_sessions`, `assistant_runs`, `assistant_action_previews` и
`assistant_feedback` защищены FORCE RLS и составным organization/workspace
ключом. Срок удаления задаётся общей retention/legal-hold политикой проекта;
удаление не должно обходить audit/outbox/inbox. Перед production admission
запустите `make test`, `make community-check`, `./scripts/check-contracts.sh`,
`npm run build` в `frontend` и сохраните synthetic screenshots страницы
`/assistant`. Скриншоты не должны содержать реальные вопросы, PII или секреты.
