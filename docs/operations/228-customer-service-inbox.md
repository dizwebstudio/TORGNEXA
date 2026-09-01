# Клиентский сервис и единый inbox: эксплуатация Task 228

## Назначение

Task 228 добавляет единую очередь для сообщений, отзывов, вопросов,
претензий и возвратных обращений. Он связывает обращение с каноническими
Order/Product/Return/Claim references, но не создаёт второй CRM, customer
master или claims ledger.

Входящий контур:

```text
connector inbound → sanitize/digest → dedup thread/message
→ queue + Customer 360 → assignment/SLA → case/return/refund reference
```

Исходящий контур:

```text
draft/internal note → approval/capability → idempotent reply intent
→ outbox/connector → receipt/read-after-write
→ sent/failed/unknown → reconciliation
```

## API, SDK, MCP и frontend

Read API требует `customer_service.read`:

- `GET /api/v1/customer-service/summary` — counters, SLA breaches и unknown replies;
- `GET /api/v1/customer-service/inbox` — bounded cursor queue;
- `GET /api/v1/customer-service/reviews` и `/questions` — typed queue views;
- `GET /api/v1/customer-service/threads/{conversation_id}` — messages/replies;
- `GET /api/v1/customer-service/customers/{customer_ref_id}` — privacy-safe timeline;
- `GET /api/v1/customer-service/findings` — reconciliation queue.

Write API использует tenant scope, permission, audit и `Idempotency-Key`:

- `POST /api/v1/customer-service/inbound` — sanitized connector ingestion;
- `POST /api/v1/customer-service/replies` — durable public/internal reply intent;
- `POST /api/v1/customer-service/assignments` — optimistic assignment;
- `POST /api/v1/customer-service/transitions` — state transition.

Go, Python и TypeScript SDK генерируются из OpenAPI. MCP предоставляет
`commerce.customer_service.get` только для чтения: agent identity задаёт
tenant/workspace, а MCP не отправляет replies и не выдаёт credentials.

В frontend раздел «Клиентский сервис» показывает вкладки всех обращений,
отзывов, вопросов и претензий, поиск, SLA/quality counters, thread detail,
customer timeline, assignment, resolve/reopen и раздельные публичный ответ и
внутреннюю заметку. Неподдержанная запись показывается как недоступная, а
`unknown` не маскируется под `sent`.

## Privacy, moderation и identity

`CustomerRef` хранит stable tenant-scoped reference, маску имени/контакта,
confidence и identity state. `verified` допускается только для approved exact
reference; похожее имя или совпавший свободный текст не объединяют клиентов.

В БД попадают sanitized text и SHA-256 digest. Raw HTML, scripts, control
characters, credentials, Authorization headers и raw provider payloads не
должны попадать в storage, event, log или audit. Вложения — только через
upload quarantine/release pipeline с object reference; прямой URL/SSRF
доступ запрещён.

Публичный ответ, внутренняя заметка и AI draft разделены на уровне модели и
SQL constraints. AI draft всегда остаётся draft. Public reply требует
permission/approval, а remote timeout переводит его в `unknown` и создаёт
reconciliation work вместо повторной отправки вслепую.

## SLA, queues и recovery

Очереди фильтруются по состоянию, типу, priority, assignee/team, customer,
SLA и unresolved. Assignment history append-only и защищён optimistic
version. SLA policy version хранит timezone, holidays и first-response /
resolution minutes; due time вычисляется детерминированно в UTC.

При duplicate webhook/poll повтор безопасен. При provider outage операции
остаются queued/unknown, а не объявляются выполненными. Оператор сначала
проверяет receipt/read-after-write, затем запускает reconciliation. Для
инцидента с публичным ответом отключается outbound kill switch, сохраняется
audit и проверяется idempotency key. История не переписывается.

## Qualification

Локальный gate:

```bash
make customer-service-qualification
```

Он проверяет migration 000055 и catalog hash, OpenAPI paths, SDK/MCP/frontend
wiring, forced RLS, append-only triggers и отсутствие secret-like полей.
Остальные unit/API/frontend/contract/migration проверки запускаются CI.

`repository-complete` не означает credentialed production qualification.
Для каждого канала отдельно сохраняются redacted evidence с connector/API
version, scopes, датой, inbound/reply, attachments/moderation, rate limits,
read-after-write, timeout/unknown и reconciliation recovery. Без этого
capability остаётся `read_only`, `not_available` или
`qualification_required`.

## Incident checklist

1. Отключить outbound replies или конкретную capability kill switch-ом.
2. Не повторять unknown reply до проверки remote receipt и idempotency.
3. Создать finding с digest/correlation ID; не сохранять raw payload.
4. Проверить tenant scope, audit и upload release state.
5. После подтверждения remote state выполнить reconciliation и только затем
   разрешить capability.
