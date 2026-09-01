# Task 228 — Клиентский сервис: единый inbox, отзывы, вопросы и претензии

## Статус

`repository-complete` — 2026-09-01. На уровне репозитория закрыт provider-neutral
контур единого inbox: нормализация и дедупликация входящих обращений, privacy-safe
CustomerRef/timeline, очереди и SLA-поля, durable reply intents, claims/returns
references, forced RLS, API/SDK/MCP, frontend и synthetic qualification gate.
Credentialed live/sandbox-проверки конкретных каналов остаются внешним
release-gate и не подменяются локальным health-check.

## Результат выполнения

Все подзадачи 228.1–228.14 закрыты на уровне repository boundary. Реализация
переиспользует канонические Conversation/Message/Case/Claim/Return/Order
агрегаты и добавляет только tenant-scoped service projection: безопасный текст,
маскированные ссылки на клиента, immutable историю и durable outbound intent.
Второй CRM, customer master или claims ledger не создаётся.

| Подзадача | Статус | Evidence |
|---|---|---|
| 228.1 | `closed` | ADR-0179, CustomerRef с conservative identity matching, privacy и retention boundary |
| 228.2 | `closed` | typed core contracts для conversation/message/reply/assignment/SLA/finding и validation tests |
| 228.3 | `closed` | inbound normalization, remote thread/message deduplication, digest и reconciliation finding storage |
| 228.4 | `closed` | bounded deterministic Customer 360 timeline с canonical order/product/return/claim references |
| 228.5 | `closed` | review/question filters, public/internal reply separation, moderation и delivery states |
| 228.6 | `closed` | typed claim/return/refund links и append-only case/reconciliation boundary |
| 228.7 | `closed` | tenant-scoped cursor inbox, assignment history и optimistic version conflict |
| 228.8 | `closed` | versioned SLA policy model, business-time calculation и explainable SLA states |
| 228.9 | `closed` | idempotent reply queue, approval/draft-only AI rule, unknown delivery state и audit |
| 228.10 | `closed` | capability-aware API/MCP surface и qualification gate с fail-closed external boundary |
| 228.11 | `closed` | migration 000055, forced RLS, bounded indexes, append-only triggers и sensitive-field checks |
| 228.12 | `closed` | OpenAPI, regenerated Go/Python/TypeScript SDK, MCP read tool и Customer Service frontend |
| 228.13 | `closed` | sanitization, permission split, redacted audit, privacy boundary, quota/kill-switch release rules |
| 228.14 | `closed` | core/API/repository/static gates, frontend checks, migration/catalog evidence и synthetic E2E contract |

`repository-complete` не означает, что конкретный marketplace или social
provider получил production-доступ. Для разрешения `replies.write`, attachments,
moderation или claims write нужны retained credentialed sandbox/live evidence:
connector/API version, scopes, дата, read-after-write, timeout/unknown и
redacted result. До этого capability остаётся `read_only`, `not_available` или
`qualification_required`.

## Цель

Сделать один безопасный inbox для всех входящих клиентских обращений:

```text
отзыв/вопрос/сообщение → единый тред → customer timeline
→ назначение и SLA → ответ/претензия → заказ/возврат/refund
→ решение → audit и аналитика качества
```

Оператор должен видеть контекст клиента, заказа и товара, отвечать из одной
очереди, понимать приоритет и дедлайн, передавать претензию в claims/returns,
а система — сохранять историю без дублей и утечки PII. Customer Service не
создаёт второй Order, Return, Payment или Claims ledger.

## Что уже есть и что закрывает этот task

- Task 057 даёт provider-neutral основу Conversation/Message/Case/Assignment/
  SLA, dedup удалённых тредов и draft-only правило для AI.
- Task 056 даёт Claims/Evidence/Deadline/Compensation с audit и SLA
  escalation.
- Task 009 даёт Inbox/Idempotency для транзакционных consumers.
- Существуют уведомления, social/classified message capabilities и
  marketplace order/return references.

Task 228 превращает эту основу в единый рабочий процесс и отдельно
квалифицирует channel-specific reviews, questions, replies, attachments,
claims и customer identity. Канал без официальной capability остаётся
`read_only`, `not_available` или `qualification_required`.

## Единая модель обращения

Каждая входящая сущность нормализуется в tenant-scoped conversation/message с
типом `message`, `review`, `question`, `claim`, `return_request` или
`delivery_failure`. Тред хранит remote channel/account/thread IDs, source
quality, timestamps, participants в минимизированном виде, ссылки на Order,
OrderItem, Product, Return и Claim, но не копирует весь профиль клиента.

Система различает:

- `unread`, `open`, `pending_customer`, `pending_internal`, `resolved`,
  `closed`, `spam`;
- priority `low`, `normal`, `high`, `urgent`;
- `new`, `in_progress`, `waiting`, `escalated`, `breached` SLA;
- `verified`, `ambiguous`, `unmatched` customer identity;
- `observed`, `draft`, `sent`, `accepted`, `failed`, `unknown` для ответа.

## Подзадачи

### 228.1 — ADR, scope и privacy-first CustomerRef

**Зависимости:** 056, 057.

- Зафиксировать ownership: Conversation/Message, Case/Claim, CustomerRef,
  Order/Return, Product и Connector mapping.
- Ввести минимизированный `CustomerRef`: stable tenant-scoped identity,
  display name/contact mask при необходимости, confidence, source и merge
  history; не строить скрытый CRM-профиль из всех сообщений.
- Определить lifecycle треда/case, порядок resolution, reopen, merge/split,
  retention, DSAR/delete/restrict и legal hold.
- Утвердить приоритеты, SLA clocks, escalation policies, roles, audit и
  approval для ответов, компенсаций и sensitive data.
- Определить, какие внешние данные считаются untrusted content и проходят
  sanitization/quarantine.

**Acceptance:** ADR показывает источник истины и retention для каждого поля;
неоднозначные клиенты не объединяются автоматически; PII minimization и
manual review rules одобрены.

### 228.2 — Unified Conversation/Message contract

**Зависимости:** 228.1, 057.

- Расширить typed contracts для message, review, question, answer, claim,
  attachment, participant, tag, assignment, SLA event и resolution.
- Для каждого сообщения хранить scoped remote identity, source, direction,
  occurred/received time, content digest, safe text, status и references.
- Поддержать thread merge/split только через explicit audited operation;
  published/received messages immutable.
- Добавить normalized source quality, language, moderation flag, spam state и
  customer identity confidence.
- Стабильные technical codes сохранить, пользовательские labels сделать
  русскими и понятными.

**Acceptance:** schema/domain tests покрывают типы, immutable history,
remote identity, duplicate/collision, locale, unsafe content, attachment
reference и cross-tenant rejection.

### 228.3 — Inbound ingestion, deduplication и reconciliation

**Зависимости:** 228.2, 009.

- Реализовать inbound adapters для официальных message/review/question
  capabilities с cursor/checkpoint, webhook verification и bounded backfill.
- Дедуплицировать по connector account + remote thread/message ID и
  canonical event fingerprint; одинаковый message из polling/webhook не
  создаёт второй объект.
- Обрабатывать edit/delete/redaction, out-of-order, late message, duplicate
  webhook, provider outage и remote thread archive.
- Не сохранять credentials, raw provider payload и несанитизированный HTML;
  вложения проходят upload quarantine/release policy.
- Создавать reconciliation findings для local/remote drift, missing mapping,
  partial sync и unknown delivery.

**Acceptance:** replay, crash/retry, webhook-before-poll, duplicate, edited
message, deleted review и partial page fixtures дают один согласованный thread
и audit trail; неавторизованный канал не создаёт запись.

### 228.4 — Customer 360 timeline без опасного профилирования

**Зависимости:** 228.1–228.3.

- Построить единый timeline по CustomerRef: conversations, reviews,
  questions, answers, orders, shipment, returns, refunds, claims и SLA events.
- Разрешать identity matching по approved exact references: remote customer ID,
  order link, verified contact token; ambiguous candidates показывать на review.
- Добавить links до Order/OrderItem/Product/Return/Claim без копирования
  полного customer payload.
- Поддержать timeline filters, period, channel, status, unresolved, SLA and
  cursor pagination; порядок событий — deterministic UTC.
- Учитывать privacy restrictions, redaction and retention при построении
  derived timeline.

**Acceptance:** два канала одного подтверждённого клиента объединяются только
  по policy; похожие имена не объединяются; timeline не показывает чужой
  заказ, PII или удалённый объект.

### 228.5 — Отзывы, вопросы и ответы по товарам/заказам

**Зависимости:** 228.2–228.4, 222, 223.

- Добавить отдельные views и lifecycle для review, product question, answer,
  rating, moderation flag, response deadline и source quality.
- Связывать отзыв/вопрос с Product, Offer/SKU и Order только при verified
  remote mapping; несопоставленный объект остаётся `unmatched`.
- Поддержать draft → approval → reply, edit/delete/retract если это разрешает
  официальная capability; сохранить remote receipt/read-after-write.
- Различать публичный ответ, приватное сообщение и внутреннюю заметку; нельзя
  случайно отправить внутренний текст клиенту.
- Учитывать язык, moderation, prohibited content и provider limits.

**Acceptance:** synthetic review/question с ответом, редактированием,
rejection, unknown remote result и missing product mapping отображаются в одной
очереди, без ложного `sent` и без повторного публичного ответа.

### 228.6 — Cases, claims, returns и компенсации

**Зависимости:** 056, 164, 228.2–228.4.

- Создавать Case из сообщения/отзыва/вопроса или претензии с категорией,
  severity, reason, linked order/item, owner, evidence и SLA.
- Переводить delivery problem, damaged item, missing parcel, wrong product,
  payment/refund issue и quality complaint в Task 056 Claim или Task 164
  Return/Refund через typed reference, не копируя агрегат.
- Поддержать evidence upload только через released/quarantined artifact,
  customer-visible и internal notes раздельно.
- Хранить предложенную компенсацию отдельно от подтверждённой payment/
  settlement операции; compensation requires policy/approval.
- Возвращать результат downstream операции в case timeline и автоматически
  закрывать case только по подтверждённому событию.

**Acceptance:** case с partial return/refund, carrier claim, evidence,
approval denial, unknown payment outcome и reopen проходит без двойной
компенсации или закрытия до фактического результата.

### 228.7 — Очереди, маршрутизация, assignment и приоритет

**Зависимости:** 228.2, 228.6.

- Добавить inbox queues по workspace, channel, product, category, language,
  priority, SLA, assignee, team, status и unresolved state.
- Реализовать manual assignment, team routing, round-robin/skill rule только
  как versioned policy; изменение владельца аудируется.
- Поддержать tags, saved views, bulk triage, merge/split и internal notes с
  ограничением по permission.
- Запретить assignment в несуществующую/неактивную команду и потерю обращения
  при конфликте версии.
- Выводить counter unread/open/breached и объяснение, почему обращение в
  очереди.

**Acceptance:** очередь tenant-scoped, фильтры cursor-paginated, повтор
assignment идемпотентен, конкурентное изменение не теряет владельца, bulk
triage не затрагивает чужие или закрытые обращения.

### 228.8 — SLA engine, календарь и эскалации

**Зависимости:** 228.1, 228.7.

- Добавить versioned SLA policies по типу обращения, каналу, priority,
  workspace/team и рабочему календарю с timezone/holidays.
- Разделить first-response, next-response и resolution timers; поддержать
  pause/resume для `pending_customer` и approved waiting states.
- Вычислять due_at, warning, breached, escalation chain и business-minute
  duration в UTC с explainable policy version.
- Создать durable scheduler/worker, который безопасно повторяется и не
  отправляет дубли уведомлений/эскалаций.
- Связывать SLA breach с notification, manager queue и case timeline.

**Acceptance:** тесты покрывают weekend/holiday/timezone, pause/resume,
priority change, reassignment, reopen, worker crash и duplicate escalation;
дедлайн воспроизводим для исторической policy version.

### 228.9 — Ответы, шаблоны, вложения и outbound delivery

**Зависимости:** 228.2, 228.5–228.8, 057.

- Сделать reply composer с public reply/internal note, template, locale,
  variable allowlist, signature и preview перед отправкой.
- Поддержать attachment references через upload security pipeline, size/MIME/
  malware checks и удаление неиспользованного draft artifact.
- Идемпотентно отправлять ответ через typed connector capability с outbox,
  receipt, rate limit и read-after-write; unknown remote result требует
  reconciliation, а не слепой повтор.
- Отделить human approved reply от AI draft; AI не имеет права отправлять,
  менять SLA, закрывать case или обещать refund.
- Проверять channel-specific length, forbidden markup, moderation и
  customer-visible audience до remote call.

**Acceptance:** повтор send не создаёт два ответа; internal note не уходит
наружу; timeout-after-accept, failed upload, provider rejection, rate limit,
approval denial и attachment quarantine видны оператору.

### 228.10 — Connector capabilities и qualification

**Зависимости:** 226, 228.3, 228.5, 228.9.

- Разделить capabilities для `messages.read`, `messages.reply`,
  `reviews.read`, `questions.read`, `answers.write`, `claims.read/write` и
  channel-specific moderation/attachments только при официальном API.
- Составить matrix для marketplace, storefront, classified/social, email/help
  channel, указав read/write, scopes, limits, webhooks, edit/delete и SLA.
- Квалифицировать первую волну минимум на одном marketplace и одном
  storefront/social source для inbound + reply + reconciliation.
- Для каждого provider проверить auth, pagination, dedup, idempotency, safe
  retry, unknown outcome, read-after-write и remote moderation result.
- Не использовать scraping, cookies или browser automation вместо connector
  API; unsupported operations показывать как `not_available`.

**Acceptance:** conformance evidence привязано к версии connector/API и дате;
manifest/SDK без exact runtime evidence не включает reply или claim write;
capability state одинаков в API, UI, worker и MCP.

### 228.11 — Persistence, search, RLS и audit lineage

**Зависимости:** 228.2–228.10.

- Добавить expand-only storage для conversations, messages, reviews, questions,
  cases, assignments, SLA timers, CustomerRef links, tags, replies, receipts,
  attachments и reconciliation findings.
- Включить `FORCE ROW LEVEL SECURITY`, organization/workspace predicates,
  optimistic versions, idempotency uniqueness и bounded indexes.
- Добавить tenant-scoped search по safe text/digest, remote ID, order/SKU,
  status, priority and dates; full raw message search не должен обходить
  privacy/retention controls.
- Хранить immutable inbound/outbound history, before/after digest, actor,
  policy, approval, remote receipt и correlation ID.
- Определить retention/anonymization for customer content, attachments,
  deleted messages, legal hold and derived Customer 360 timeline.

**Acceptance:** migration/repository/RLS/search tests проходят; cross-tenant,
  restricted-content, append-only, idempotency, retention and attachment access
  violations fail closed.

### 228.12 — API, SDK, UI и support analytics

**Зависимости:** 228.7–228.11.

- Добавить cursor API для inbox, thread, customer timeline, review/question,
  case, assignment, SLA, reply, search, bulk triage and reconciliation.
- Обновить OpenAPI и generated SDK; mutation routes требуют Idempotency-Key,
  expected version, permission, approval и safe error code.
- Сделать UI с колонками «Новые», «В работе», «Ожидание клиента», «Нарушен SLA»,
  «Отзывы», «Вопросы», «Претензии» и «История клиента».
- В thread detail показать order/product/return/refund context, timeline,
  SLA countdown, assignment, public/internal composer, attachments and remote
  delivery state.
- Добавить analytics: first response time, resolution time, SLA breach,
  backlog, reopen, CSAT/review response rate и channel quality без публикации
  непроверенных customer metrics.

**Acceptance:** keyboard/focus, responsive inbox, filters, retry, empty/error/
  unknown states, bulk triage, public/internal separation, customer timeline
  and SLA actions проходят authenticated browser tests.

### 228.13 — Security, moderation, observability и recovery

**Зависимости:** 228.3, 228.8–228.12.

- Ввести moderation/sanitization for HTML, links, scripts, prompt injection,
  attachments, fraud/spam and prohibited personal data.
- Разделить permissions для чтения PII, ответа, claim, compensation,
  attachment download, export, assignment и SLA policy.
- Метрики: ingest lag, duplicate suppression, reply latency, delivery unknown,
  queue age, SLA breach, sync drift, attachment failure, provider rate limit и
  reconciliation age.
- Добавить redacted logs, audit, per-workspace quotas, kill switch outbound
  replies и runbooks для duplicate/unknown delivery, accidental public reply,
  provider outage и PII incident.
- Безопасно восстанавливать inbox после outage; pending outbound нельзя
  отправлять повторно без проверки receipt/idempotency.

**Acceptance:** security tests подтверждают redaction, SSRF/upload controls,
  tenant isolation, permission denial, kill switch и recovery без двойного
  ответа; untrusted client text не меняет policy или system instructions.

### 228.14 — Demo, E2E, load и release gate

**Зависимости:** все предыдущие подзадачи.

- Добавить synthetic customer с двумя каналами, заказом, товаром, отзывом,
  вопросом, сообщением, претензией, возвратом/refund, attachment и SLA policy.
- Пройти сценарий inbound → dedup → customer timeline → assignment → reply →
  review answer → claim → return/refund link → SLA escalation → resolve.
- Проверить duplicate/out-of-order webhook, edit/delete, ambiguous identity,
  cross-tenant access, PII redaction, public/internal mix-up, crash after
  remote accept, provider rejection, rate limit, unknown delivery и reopen.
- Добавить authenticated browser E2E и Compose API/worker E2E с synthetic
  connectors; bounded load smoke минимум на 1 000 conversations.
- Release допускает `replies.write`/claim writes только для connectors с
  актуальным conformance/live or sandbox evidence; остальные остаются read-only.

**Acceptance:** demo inbox сохраняет единую историю без дублей, SLA правильно
эскалирует, ответы не повторяются, претензия связана с downstream case,
неизвестные операции видны оператору, а production claim подтверждён
retained evidence на целевой topology.

## Архитектурные ограничения

- Customer Service связывает canonical references с Order, Product, Return,
  Refund и Claim, но не создаёт их копии или второй customer master без ADR.
- PostgreSQL — источник операционной истины; inbound/outbound effects идут
  через Inbox/Outbox, workers idempotent и lease-fenced.
- Все данные tenant/workspace-scoped; customer identity matching и объединение
  истории разрешены только approved exact references, не по догадке.
- Внешний клиентский текст и вложения — untrusted input; sanitization,
  quarantine, malware/SSRF controls и prompt-injection boundaries обязательны.
- Secrets, raw provider payloads, full payment credentials и unnecessary PII не
  сохраняются в events, logs, audit, fixtures или обычных columns.
- AI/MCP/n8n могут читать разрешённый контекст и готовить draft, но не могут
  отправлять ответ, подтверждать компенсацию или обходить SLA/approval/policy.

## Не входит в этот task

- Полноценный CRM, маркетинговая сегментация и рекламное профилирование.
- Автоматическое принятие юридических претензий или финансовых компенсаций
  без claims/payment approval.
- Scraping закрытых кабинетов и неофициальные API.
- Контент-модерация как отдельный trust-and-safety продукт; здесь только
  необходимые блокировки и маршрутизация.

## Зависимости

009, 020, 022, 056, 057, 164, 217, 220, 223, 226.

## Definition of Done

- Все 14 подзадач имеют implementation, contracts/docs и success/failure/
  idempotency tests.
- Единый inbox, customer timeline, reviews/questions, claims, SLA, replies,
  UI/API/SDK и reconciliation используют одну capability/policy matrix.
- Публичный ответ, внутреннее сообщение, AI draft и неизвестный remote result
  не смешиваются и не создают двойной delivery.
- RLS, privacy/retention, moderation, audit, quotas, kill switch,
  authenticated E2E и connector qualification evidence сохранены.
- Пройдены `gofmt`, `go test ./...`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `make architecture`, `make migrations`,
  frontend typecheck/build и connector conformance на целевой topology.
