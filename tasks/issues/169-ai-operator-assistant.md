# Task 169 — AI-помощник для оператора

## Статус

`planned` — подробная декомпозиция подготовлена, реализация не начата.

## Цель

Добавить в TORGNEXA безопасного AI-помощника для оператора, который отвечает
на вопросы по текущему состоянию магазина и предлагает следующий проверяемый
шаг. Помощник должен объяснять факты из tenant-scoped контуров каталога,
товаров, публикации, запасов, заказов, возвратов, интеграций, уведомлений,
юнит-экономики и рабочих процессов, указывать источники и их свежесть, а при
необходимости формировать типизированный preview действия.

Это операторский copilot, а не новый источник истины, автономный исполнитель,
универсальный чат с базой данных или скрытый административный интерфейс. Модель
может предложить действие, но никогда не получает права оператора: действие
повторно проверяется обычным permission/RBAC/ABAC, capability, policy,
approval, version, idempotency, audit и Outbox/Inbox-контуром.

Первый вертикальный срез должен позволять оператору спросить:

1. **«Что требует внимания?»** — по Единому центру состояния интеграций
   (Task 168), sync/retry/DLQ, уведомлениям и reconciliation;
2. **«Почему товар не публикуется?»** — по Центру качества публикации (Task
   166) с конкретными blockers/warnings и ссылкой на карточку товара;
3. **«Какие каналы просели или убыточны?»** — по фактическому отчёту Task 167,
   без пересчёта финансовых фактов моделью;
4. **«Что будет с остатком и когда пополнять?»** — по forecast/evidence Task
   165, с явным отличием рекомендации от факта склада;
5. **«Сформируй план исправления»** — только типизированный, ограниченный
   preview с текущим capability и approval status, без автоматического запуска.

## Текущий разрыв

- `frontend/src/features/reports/AskAI.tsx` отправляет на
  `/api/v1/settings/ai-providers:analyze` caller-assembled `system_prompt`,
  prompt и `data_classes`, собранные из усечённого отчёта. Сервер не знает
  намерение вопроса, не строит source context и не возвращает citations,
  freshness или grounding state.
- `internal/app/api/ai_advisory.go` — bounded completion для выбранного
  provider account. Ответ содержит только `text/provider/model`; нет session,
  run, evidence, feedback, cancellation, action preview или безопасной
  рекомендации.
- Provider account/SecretProvider/egress governance уже защищают внешний
  completion-вызов, но не превращают произвольный prompt в операторский
  продуктовый ответ. Текущий endpoint должен остаться совместимым для
  существующего Ask AI до миграции, но не может быть скрытым backdoor для
  нового помощника.
- Task 079 разделяет application permission и immutable AI-agent policy,
  помечает source facts как `UNTRUSTED_TOOL_DATA`, запрещает секреты и требует
  approval для sensitive write. Помощник обязан использовать те же правила;
  пользовательский текст, текст товара, отзыв, webhook, connector response и
  ответ модели не могут задавать tenant, роль, лимит, policy или approval.
- Task 168 даст единый permission-aware статус интеграций, Task 166 —
  quality receipt, Task 167 — фактическую юнит-экономику, Task 165 — forecast,
  Task 164 — возвраты/refunds, Task 163 — типизированные workflow. Помощник
  должен читать их через канонические порты и ссылаться на их snapshot/evidence,
  а не копировать доменную логику в AI-модуль.
- `cmd/mcp` сейчас остаётся deny-by-default, пока production composition не
  подключит реальный Governor/Auditor Task 079/084. Помощник не должен
  обходить этот барьер отдельным MCP, n8n или frontend-путём.

## Продуктовый контракт

### Что получает оператор

Ответ состоит из нескольких независимых частей:

- короткий ответ на русском языке без chain-of-thought;
- структурированные факты и вычисления, возвращённые authoritative-модулем;
- `grounding_state`: `grounded`, `partially_grounded`, `insufficient_data`,
  `stale_data`, `source_unavailable`, `refused`;
- bounded `evidence[]` с типом источника, ссылкой на разрешённый экран/ресурс,
  `observed_at`, `checked_at`, TTL/возрастом, `source_version`, visibility и
  digest — без raw provider payload, токенов, prompt или персональных данных;
- уровень покрытия ответа источниками и безопасное объяснение, почему данных
  недостаточно; модельное «confidence» само по себе не является доказательством;
- `recommendations[]` с причиной, ожидаемым эффектом, ограничениями и
  следующей ссылкой, но без скрытого исполнения;
- `action_previews[]` только для действий из серверного action catalog.

Ответ не должен показывать внутреннее рассуждение модели, системный prompt,
скрытые tool calls или credential-shaped строки. Markdown/HTML/ссылки от
модели проходят safe renderer: arbitrary HTML, javascript/data URL и внешние
неразрешённые redirect-ссылки запрещены.

### Состояния session/run

Session tenant- и actor-scoped, а run относится к одной версии контекста и
одному correlation id. Разрешённые состояния run:

`queued`, `retrieving_context`, `awaiting_model`, `streaming`,
`awaiting_approval`, `action_queued`, `completed`, `partial`, `stale`,
`blocked`, `provider_unavailable`, `cancelled`, `failed`.

Переходы монотонны и версионированы. Таймаут или потеря worker lease не
означает `completed`; неизвестный внешний outcome остаётся pending/unknown и
переходит в существующий reconciliation/manual-attention контур.

### Источники и доверие к контексту

Сервер сам определяет intent, необходимые data classes и набор источников.
Выбранная оператором страница, фильтр или entity id — только подсказка для
retrieval и повторно проверяется правами/tenant scope; они не дают полномочий.
Каждый retrieved fact несёт:

- `source_kind`, `source_ref`, `source_version`, `observed_at`, `watermark`;
- `freshness`: `fresh`, `stale`, `missing`, `redacted`, `unavailable`;
- `context_trust=untrusted_tool_data` для внешнего/модельного текста;
- bounded `evidence_digest`, tenant visibility и безопасный deep link.

Не допускается считать отсутствие строки нулём, здоровым состоянием или
разрешением операции. Финансовые, складские, compliance и юридически значимые
ответы явно показывают basis, currency/quantity semantics и неполноту.

## Модель полномочий и действий

| Класс | Поведение помощника | Примеры |
|---|---|---|
| `read` | Читает только разрешённые агрегаты и evidence | статус интеграции, ошибки sync, отчёт |
| `safe_write` | По умолчанию только draft/preview; запуск возможен лишь через существующий typed API и policy | отметить уведомление, запланировать безопасный dry-run |
| `sensitive_write` | Только сформировать Task-017 approval request; модель и помощник не исполняют действие | изменение цены, публикация, refund, отправка workflow |
| `prohibited` | Всегда отказ | секреты, токены, private keys, SQL/shell/HTTP, экспорт PII |

Каждый preview содержит canonical action, resource, expected version, risk,
required permissions, current capability/runtime evidence, approval requirement,
bounded impact, idempotency key и `expires_at`. При подтверждении оператором
сервер заново читает source и policy; устаревший preview получает conflict и
не выполняется. Все side effects идут через владельца домена, не через
провайдерскую ветку или AI worker.

## Подзадачи и порядок реализации

### 169.1 — ADR, scope, threat model и definition of done

**Зависит от:** нет.

- Зафиксировать, что помощник — provider-neutral advisory/read surface над
  каноническими модулями; legacy completion и operator assistant имеют разные
  контракты и permission scopes.
- Утвердить supported intents, allowed data classes, states, retention,
  locale, answer limits и правило «нет evidence — нет утверждения».
- Провести threat model для prompt injection, tool/data exfiltration,
  cross-tenant leakage, malicious links, model hallucination, cost abuse,
  replay, stale approvals и provider outage.
- Согласовать границы с Tasks 017, 022, 053, 063, 079, 082, 089b, 122,
  126, 163–168 и ADR-0008/0021; добавить отдельный ADR, не переписывая
  существующую AI-agent policy.

**Приёмка:** ADR содержит state machine, authority matrix, abuse cases,
compatibility plan с `/settings/ai-providers:analyze` и RUNTIME-169 gate.

### 169.2 — Canonical assistant/session/run contracts

**Зависит от:** 169.1.

- Создать provider-neutral Go structs и Draft 2020-12 schemas для
  `AssistantSession`, `AssistantMessage`, `AssistantRun`, `Answer`,
  `EvidenceRef`, `Recommendation`, `ActionPreview`, `Feedback` и `RunError`.
- Ограничить длину вопроса/ответа, число сообщений, facts, citations,
  recommendations и action previews; UUIDv7/ULID, UTC и canonical enums.
- Разделить user input, server intent, retrieved facts, model answer и
  executed action; не использовать `map[string]any` для стабильной части.
- Добавить `contract_version`, `context_digest`, `answer_digest`,
  `ai_generated` и `output_kind`: source facts остаются `source_facts`, а
  model-authored summary/recommendation явно помечается
  `ai_generated=true`/`ai_recommendation`.

**Приёмка:** schema/property tests отвергают неизвестные states, cross-tenant
refs, oversized payloads, secret-shaped values и `completed` без bounded answer
и evidence state; additive changes следуют compatibility policy.

### 169.3 — Privacy, data classification, retention и redaction

**Зависит от:** 169.1–169.2.

- Утвердить классы `aggregate`, `operational`, `financial`, `inventory`,
  `personal`, `credential`, `regulated` и egress policy для каждого provider
  account; default — minimum aggregate context.
- Определить, что хранится в PostgreSQL, что остаётся в короткоживущем
  worker buffer, а что доступно только в ответе UI. Raw prompt, raw provider
  response, chain-of-thought и tool payload не попадают в audit/events/logs.
- Для включаемой истории диалога хранить bounded redacted transcript либо
  шифрованный tenant-owned artifact с явным retention/legal hold; удалить его
  можно только через действующий privacy workflow.
- Повторно применять `trustcontrol.RedactPrompt`, secret-shaped rejection,
  PII minimization и no-store/cache headers; запрещать credential-like output.

**Приёмка:** redaction corpus, retention/erasure tests и log/event scans не
  находят tokens, Authorization headers, private keys, raw provider payloads или
  ненужный PII; legal hold блокирует преждевременное удаление.

### 169.4 — Provider/model registry, routing и бюджет

**Зависит от:** 169.1–169.3, Task 122.

- Определить allowlisted model profiles: capability, context/output limits,
  timeout, price/cost class, supported data classes, locale и reliability.
- Account/model selection не должен быть authority из client input: операторская
  настройка — только preference, сервер проверяет account status, egress policy,
  kill switch и model profile перед каждым run.
- Для Ollama/LM Studio/Open WebUI и внешних провайдеров использовать один
  provider-neutral port; local endpoint остаётся untrusted и не получает DB или
  secret access.
- Fallback между моделями только по явной policy, с отдельной egress/audit
  evidence и без молчаливого повторного отправления чувствительного контекста.
- Ввести per-tenant/workspace/actor budgets: tokens/bytes, runs/minute,
  concurrent runs, cost ceiling и daily cap.

**Приёмка:** неизвестная модель, отключённый account, недопустимый data class,
  budget exhaustion, redirect/private egress или kill switch fail closed;
  provider registry не содержит hard-coded веток в Core.

### 169.5 — Intent classifier и question policy

**Зависит от:** 169.2–169.4.

- Реализовать bounded deterministic classifier/router для intents: integration,
  sync, product-quality, inventory/forecast, order/return, unit-economics,
  report-summary, notification, workflow-draft и unsupported.
- Отделить вопрос оператора от инструкции в retrieved text; intent, tenant,
  actor, permissions, risk и requested data classes вычисляются сервером.
- Запрещать retrieval/action при ambiguous intent, unauthorized entity,
  unsupported provider capability, stale financial basis или missing policy;
  вместо этого вернуть уточнение/`cannot_answer`.
- Не использовать keyword-filter как единственную prompt-injection защиту;
  полагаться на структурное разделение authority и untrusted context по Task
  079.

**Приёмка:** corpus на русском/английском с adversarial product/review/webhook
  text не меняет intent/tenant/risk; unsupported/sensitive questions получают
  детерминированный безопасный ответ.

### 169.6 — Retrieval ports и grounded context builder

**Зависит от:** 169.2, 169.5, Tasks 164–168.

- Определить typed read ports для Catalog/PIM/Offer, Product Quality, Inventory
  Forecast, Orders/Returns, Integration State Center, Sync/Reconciliation,
  Notifications, Unit Economics, Reports, Approval/Workflow и audit metadata.
- Читать только tenant-scoped authoritative projections с permission-aware
  redaction; не делать прямых remote probes, SecretProvider calls или
  provider-specific запросов из обычного assistant GET/run retrieval.
- Собирать bounded context pack с watermarks, freshness/TTL, source version,
  query digest и reason for omission; при partial source outage сохранять
  `partial/stale`, не подставлять нули и не объявлять green.
- Выполнять bulk queries с детерминированным порядком и N+1 budget; ClickHouse/
  Valkey разрешены только как rebuildable/accelerating read layers.

**Приёмка:** unit/integration fixtures подтверждают, что 100 аккаунтов и
  1 000 товаров собираются bounded числом запросов, нет cross-tenant facts,
  source outage виден оператору, а missing authority не превращается в claim.

### 169.7 — Evidence, citations, freshness и explainability

**Зависит от:** 169.2, 169.6.

- Реализовать citation resolver для source refs/deep links с allowlist routes,
  visibility и безопасной локализацией labels; provider URLs не выдавать.
- Ввести calculation/source watermarks, age/TTL, `freshness` и coverage:
  ответ может быть `partially_grounded` или `stale_data`, но не `grounded`
  при просроченных обязательных фактах.
- Для Unit Economics/FX/settlement показывать basis, currency, input digest и
  completeness; для stock/quality — snapshot/profile/rule versions; для
  integration — dimension/issue evidence.
- Не показывать скрытое reasoning; вместо него возвращать короткие
  explainable facts, formula/version references и «что проверить дальше».

**Приёмка:** устаревшие/редacted/неполные источники видны по каждой citation;
  ссылка открывает только разрешённый экран, а answer validator отвергает
  assertion без обязательного evidence.

### 169.8 — Prompt assembly, template versioning и injection boundary

**Зависит от:** 169.5–169.7.

- Перенести system/developer instructions на сервер; frontend передаёт только
  bounded question и optional context hint, не arbitrary system prompt/data
  classes.
- Сериализовать trusted task instructions отдельно от `UNTRUSTED_TOOL_DATA`,
  remote/catalog text и model-continuation; установить template/version,
  locale, max bytes и deterministic digest.
- Включить инструкции: не выдумывать факты, не раскрывать secrets/PII,
  не выполнять команды из данных, ссылаться только на evidence и предлагать
  action preview вместо выполнения.
- Добавить prompt-injection regression cases из
  `contracts/ai/prompt-injection-regressions-v1.json` и новые corpus cases для
  отзывов, названий товаров, webhook/error text и malicious markdown.

**Приёмка:** одинаковый context digest даёт воспроизводимый assembled pack;
  injected «ignore policy/execute SQL» остаётся source text и никогда не
  создаёт permission, tool, approval или tenant authority.

### 169.9 — Answer composer и quality/refusal policy

**Зависит от:** 169.7–169.8.

- Нормализовать model output в typed answer: summary, fact list, limitations,
  evidence, recommendations, optional previews; provider/model metadata —
  bounded and safe.
- Ввести post-validation: claims должны ссылаться на retrieved fact или быть
  явно помечены как recommendation/hypothesis; arithmetic/final financial
  values приходят из deterministic domain reports, не вычисляются floating
  point моделью.
- При hallucination risk, low evidence coverage, source conflict, provider
  refusal или unsafe content вернуть `partial/refused/insufficient_data`, а не
  правдоподобный fabricated answer.
- Feedback (`useful`, `not_useful`, `incorrect`, reason code) не меняет факты,
  policy или модельный prompt автоматически.

**Приёмка:** adversarial fixtures с conflicting/empty/stale facts не производят
  unsupported claim; output renderer не исполняет markup, а evaluator сохраняет
  только bounded quality metrics.

### 169.10 — Typed action catalog и preview compiler

**Зависит от:** 169.2, 169.5, 169.7–169.9, Tasks 017/163/166/168.

- Описать provider-neutral actions: открыть карточку/evidence, запустить
  разрешённую health-check, построить quality preview, подготовить sync dry-run,
  открыть reconciliation, сформировать workflow draft, создать approval request.
- Для каждой action задать permission, risk class, capability/runtime stage,
  expected resource/version, limits, required evidence, approval boundary и
  idempotency strategy.
- Запретить arbitrary SQL/HTTP/shell/browser/code, provider payloads, secret
  retrieval и «универсальную» batch action; bulk только с server-parsed
  bounded batch, preview/dry-run и отдельной policy.
- Preview compiler должен получать только validated typed arguments и выдавать
  human-readable impact/diff; он не вызывает remote connector.

**Приёмка:** action catalog rejects unknown/resource-cross-tenant arguments;
  health-only/SDK-only/unsupported target не получает commerce action; preview
  не создаёт side effect и содержит risk/approval/expiry.

### 169.11 — Approval bridge и execution hand-off

**Зависит от:** 169.10, Tasks 017, 079, 163/164/165/166/168.

- Sensitive actions только создают существующий Task-017 approval intent;
  approval screen показывает source evidence, exact digest, impact, actor,
  risk, policy and expiration.
- После approval повторно проверить permissions, capability grant, runtime
  qualification, quality receipt, stock/financial freshness, kill switch и
  expected version; stale preview получает conflict.
- Execution hand-off вызывает canonical domain command/worker. AI assistant не
  принимает connector credentials и не является provider router.
- Unknown remote outcome, worker crash или duplicate click переходят в
  reconciliation/pending outcome; не делать blind retry refund/payment,
  publication, shipment или price write.

**Приёмка:** sensitive write невозможно провести без Task-017; approval replay
  другой intent отклонён; duplicate/idempotency и version conflict тесты дают
  один outcome без двойного side effect.

### 169.12 — PostgreSQL persistence, RLS, lineage и retention

**Зависит от:** 169.2–169.3, 169.10–169.11.

- Добавить tenant-scoped forced-RLS tables для sessions, bounded messages,
  runs, context/evidence refs, answer summaries, action previews, feedback и
  idempotency receipts; миграция additive, backup/rehearsal gated.
- Хранить digests/source refs/versions/watermarks вместо raw provider payload;
  separate encrypted transcript only under explicit retention policy.
- `organization_id/workspace_id/actor_id` не принимаются как authority из
  client payload; cross-tenant reference и stale version fail closed.
- Добавить lineage links к report/quality/forecast/integration/approval facts,
  retention class, legal hold и rebuild marker; derived rows пересоздаваемы.

**Приёмка:** FORCE RLS tests, migration static checks, rollback/backup rehearsal
  и lineage/retention tests проходят; DB dump/search не содержит secrets/raw
  prompts/provider responses.

### 169.13 — Durable run worker, queue, cancellation и streaming

**Зависит от:** 169.4–169.12.

- Реализовать durable PostgreSQL lease/inbox/outbox worker для context retrieval,
  provider call, answer validation и preview generation; duplicate delivery
  converges to one run.
- Enforce per-workspace queue/concurrency/token/byte limits, timeout, jittered
  retry только для retry-safe provider failures, DLQ/manual replay и
  cancellation; no unbounded context or history growth on small VPS.
- Optional SSE/streaming отдаёт только ephemeral answer chunks/metadata; final
  state хранится и возобновляется через run API, side effects не запускаются
  из stream. При отсутствии stream capability использовать polling.
- Tenant/agent/provider kill switch и policy-store outage fail closed; worker
  не читает секреты вне callback-scoped connector runtime.

**Приёмка:** crash/lease loss/restart, duplicate/out-of-order event, cancel,
  provider timeout, DLQ replay и budget exhaustion имеют детерминированные
  состояния; queue/memory/connection/Kafka lag остаются в Compose limits.

### 169.14 — Canonical events, audit и notification integration

**Зависит от:** 169.11–169.13, Tasks 003, 009, 022.

- Ввести versioned events `ai.operator_assistant.run_requested.v1`,
  `...run_completed.v1`, `...action_previewed.v1`, `...approval_requested.v1`,
  `...run_failed.v1` через EventBus/Transactional Outbox/Inbox.
- Events содержат bounded IDs, state, digests, source watermarks and
  correlation/causation; никогда raw question/answer, PII, prompt, tool
  payload или credentials.
- Audit entries фиксируют actor/session/run/action/risk/policy/model/evidence
  digest и outcome; redaction и audit-failure semantics наследуются от
  существующего boundary.
- Critical provider/source failure и approval-required outcome идут в
  Notification Center Task 022 с dedupe/severity monotonicity, не в отдельную
  AI inbox.

**Приёмка:** schema/event contract, outbox/inbox dedupe, audit-before-side-effect
  и notification dedupe/escalation tests зелёные; replay не удваивает run/action.

### 169.15 — REST/OpenAPI/SDK API

**Зависит от:** 169.2, 169.5–169.14.

- Добавить contract-first endpoints с cursor/ETag/limits, например:
  `POST /api/v1/assistant/sessions`, `GET /api/v1/assistant/sessions`,
  `GET /api/v1/assistant/sessions/{id}`,
  `POST /api/v1/assistant/sessions/{id}/messages`,
  `GET /api/v1/assistant/runs/{id}`,
  `POST /api/v1/assistant/runs/{id}:cancel`,
  `POST /api/v1/assistant/action-previews/{id}:approve` через общий approval
  API и `POST /api/v1/assistant/feedback`.
- Сессия/сообщение принимают только вопрос, locale и validated context hint;
  provider/account/system prompt/data classes/tenant selectors не являются
  свободными полями authority.
- Проверять `assistant.read`, `assistant.ask`, `assistant.preview`,
  `assistant.feedback` и обычные domain permissions; capability-aware redaction
  должна сохранять отдельные denied/unknown состояния.
- Генерировать SDK, описать error/problem codes (`stale`, `blocked`,
  `approval_required`, `source_unavailable`, `budget_exceeded`, `conflict`) и
  не возвращать raw provider error.

**Приёмка:** OpenAPI/schema/SDK parity, auth/RLS/cursor/ETag/response-bound
  tests; anonymous, cross-tenant, unauthorized action, stale preview и
  idempotent retry cases fail closed.

### 169.16 — Operator UI и доступность

**Зависит от:** 169.7, 169.9–169.15, Task 168 UI.

- Добавить русскоязычную панель/страницу «Помощник оператора» из shell с
  выбором контекста текущего экрана, вопросом, списком runs и отменой;
  existing Reports → Ask AI перевести на assistant API без ломки старой
  отчётной ссылки.
- Показать answer, grounding state, freshness badges, citations/deep links,
  source limitations, recommendations, action preview impact/risk/approval и
  final outcome. Provider/model показывать только как безопасную metadata.
- Разделять «факт источника», «рекомендация ИИ», «требует подтверждения» и
  «операция недоступна»; не показывать зелёный статус при stale/unknown.
- Safe Markdown renderer, keyboard navigation, focus/error states, no raw HTML,
  responsive small-VPS/mobile layout, long text/100 citations pagination,
  accessible labels and no leaking data in URL/localStorage.

**Приёмка:** visual/browser tests cover no provider configured, partial/stale
  sources, provider error, refusal, approval, blocked/health-only target,
  long answer, reload/resume/cancel, keyboard/mobile and Russian copy.

### 169.17 — MCP/OpenClaw/n8n and external surface boundary

**Зависит от:** 169.10–169.15, Tasks 079/084/126.

- Не публиковать assistant action tools в MCP до production Governor/Auditor
  composition; valid client token alone не открывает новый путь.
- После закрытия deny-by-default добавить только provider-neutral read/query и
  preview tools, с trusted agent metadata, exact policy/risk, hard limits,
  provenance and `UNTRUSTED_TOOL_DATA`; no direct execute tool.
- n8n/Webhooks получают только versioned API/event projection, не secrets,
  hidden prompt или privileged bypass; outbound delivery остаётся Task 063.
- OpenClaw/agent requests не могут задавать organization/workspace,
  agent/model/run/integration authority, approval или batch limit.

**Приёмка:** tools/list/call tests show no tools while Governor/Auditor
  unavailable; after composition, discovery and call re-authorize exact scope,
  policy, kill switch and approval, and provenance schema validates.

### 169.18 — Security, observability, SLO и runbooks

**Зависит от:** 169.3–169.17.

- Threat tests for prompt injection, malicious source text, tenant enumeration,
  evidence/link spoofing, transcript export, credential inference, provider
  fallback, replay and model-cost exhaustion.
- Metrics without content: queue/run latency p50/p95, context query count,
  source freshness/coverage, refusal/partial/stale rate, provider/egress errors,
  token/byte/cost budgets, cancellation, approval wait, action conflicts,
  notification dedupe, DLQ age and kill-switch activations.
- SLOs state dataset size/concurrency and small-VPS ceilings: max context bytes,
  max facts/citations, runs/tenant/minute, max concurrent providers, memory,
  DB/Kafka lag, stream lifetime and response bytes.
- Runbooks for provider outage, stale source, bad/hallucinated answer,
  prompt-injection incident, runaway spend, queue/DLQ, migration/retention,
  approval timeout, cross-tenant suspicion and emergency tenant/agent/provider
  kill switch. Logs contain safe machine codes, not prompt content.

**Приёмка:** dashboards and alerts are actionable and bounded; chaos/load tests
  keep resource/cost limits; incident response can stop assistant without
  blocking ordinary commerce writes.

### 169.19 — Connector/domain readiness and deterministic demo data

**Зависит от:** 169.6, Tasks 163–168.

- Define source adapter readiness matrix: which intents are available for
  generic commerce, health-only/separate-surface connectors, integration center,
  quality, forecast, returns, unit economics and reports.
- If a connector is SDK-only, health-only, unsupported or stale, assistant says
  exactly that and never claims domain execution; surface-specific AI providers
  remain only model transports, not commerce tools.
- Seed synthetic demo tenant data for healthy/degraded/stale integration,
  failed publication rule, low-stock forecast, partial return/refund,
  mixed/partial unit economics, pending approval and provider outage.
- Add fixture manifest with source watermarks, expected answers, expected
  refusals and no production PII; demo data must be disposable in Compose.

**Приёмка:** every supported intent has at least one grounded fixture and one
  insufficient/stale/blocked fixture; SDK-only connector never yields an
  executable action preview.

### 169.20 — Full tests, Compose qualification, screenshots and docs

**Зависит от:** 169.1–169.19.

- Unit/property tests for classifier, contracts, redaction, evidence coverage,
  freshness, action risk, preview digest, state machine, budgets and reducer.
- Contract/OpenAPI/SDK/event tests; PostgreSQL migration/FORCE RLS/lineage/
  retention tests; provider mocked tests for retries, unknown outcomes,
  idempotency, callback-scoped secret and no-raw-payload behavior.
- API/worker tests for tenant isolation, permission-aware partial context,
  source outage, lease loss, duplicate events, cancellation, DLQ replay,
  approval expiry and stale preview conflict.
- Adversarial prompt-injection/regression suite: product/review/webhook text
  attempts to reveal secrets, change tenant, execute SQL, bypass approval or
  mark unsupported connector as healthy.
- Docker Compose E2E on the small VPS profile: configure one external/mock and
  one local AI provider, ask all first-slice questions, restart worker, stop
  provider, expire evidence, approve/reject preview, verify Notification Center
  and integration deep links; retain synthetic IDs, digests and screenshots.
- Update `docs/` with operator guide, supported intents/source matrix, data
  handling/retention, provider setup, troubleshooting, approval semantics,
  prompt-injection limitations, exact startup commands and screenshots matching
  shipped Russian UI. Run `gofmt`, `go test ./...`, `go vet ./...`, contract,
  architecture, migration, frontend, conformance, performance and Compose
  checks; retain release evidence.

**Приёмка:** all tests prove no false fact, no cross-tenant data, no direct
provider/secret access, no unapproved side effect and deterministic recovery;
documentation and screenshots match the released API/UI before production
admission.

## Порядок поставки

1. **Foundation:** 169.1–169.4 — ADR, contracts, privacy and model/egress
   policy; no UI or action execution yet.
2. **Grounded read slice:** 169.5–169.9 and 169.19 — intent, retrieval,
   citations, safe answer/refusal and deterministic demo fixtures for
   integration, publication quality, reports, inventory and returns.
3. **Governed operator workflow:** 169.10–169.14 — typed previews, approval
   bridge, durable runs, events, audit and notifications.
4. **Product surface:** 169.15–169.18 — API/SDK, Russian UI, MCP boundary only
   after Governor/Auditor composition, quotas and operational runbooks.
5. **Release:** 169.20 — full tests, Compose/E2E, screenshots, docs and
   evidence; first release remains recommendation/preview-first.

## Явно исключено из Task 169

- autonomous execution, hidden administrator role, model-granted permissions,
  client-selected tenant/agent/policy/approval, direct DB/provider/connector/
  SecretProvider access and any AI-only authorization path;
- raw prompt/response, chain-of-thought, provider payload, access/refresh token,
  Authorization header, private key, credential, unnecessary PII or secrets in
  PostgreSQL, ClickHouse, Valkey, Kafka, audit, logs, screenshots or exports;
- arbitrary SQL/HTTP/shell/browser/code, scraping, arbitrary plugin loading,
  unrestricted batch mutation and direct payment/refund/shipment/publication/
  price/stock/compliance write from the assistant;
- treating model confidence, generated text, successful ping, health-only or
  SDK-only connector as authoritative business fact or executable capability;
- zero-filling missing financial/stock/quality data, silently mixing currencies,
  bypassing Task-082 compliance, Task-017 approval, connector conformance,
  reconciliation, outbox/inbox, idempotency or kill switches;
- replacing Notification Center, Integration State Center, quality/forecast/
  economics/report sources, MCP governance, workflow engine or domain state
  machines with a second AI-owned source of truth;
- training/fine-tuning on tenant content, cross-tenant retrieval, public chat
  sharing or external analytics/telemetry of prompt/answer content without a
  separately approved privacy/commercial task.

## Gate RUNTIME-169

- assistant answers are tenant/actor scoped, versioned, bounded and grounded in
  authoritative source evidence with source refs, watermarks, freshness,
  visibility and digest; `insufficient_data`, `stale`, `partial`, `blocked` and
  `refused` are never presented as a confident fact;
- server, not model or frontend, determines tenant, permissions, intent,
  data classes, risk, source set, provider/model eligibility and action limits;
  external/model text is structurally `UNTRUSTED_TOOL_DATA` and cannot grant
  authority;
- current completion API remains compatible but arbitrary client system prompt
  and data-class claims cannot become the assistant path; prompt templates,
  provider routing, egress and budgets are versioned and governed;
- no raw prompt/response, chain-of-thought, secret, token, private key, raw
  provider payload or unnecessary PII appears in persistent state, events,
  audit, logs, UI URLs, screenshots or exports; retention/legal hold and
  deletion are tested;
- action catalog is typed/provider-neutral and preview-only by default; every
  write reuses current capability/runtime/quality/compliance/policy checks,
  Task-017 approval where sensitive, expected version, idempotency, audit and
  canonical domain worker; unknown outcome never blindly retries;
- worker/session/run/event/outbox/inbox/notification paths are duplicate,
  out-of-order, crash, lease-loss, cancellation, DLQ and replay safe; source
  outage produces partial/stale evidence, not false health or completion;
- API/OpenAPI/SDK/UI provide accessible Russian operator workflow, citations,
  deep links, safe rendering, permission-aware redaction, cursor/ETag/bounds,
  resume/cancel and clear distinction between fact, AI recommendation,
  approval-required and unavailable operation;
- MCP/OpenClaw/n8n remain additive and deny-by-default until trusted
  Governor/Auditor composition; no new privileged path or hidden provider
  credentials is introduced;
- small-VPS Compose quotas bound context, provider calls, tokens, memory,
  connections, queue/Kafka lag, SSE and database queries; dashboards, alerts,
  kill switches and runbooks enable recovery without stopping commerce;
- all unit/property/contract/RLS/security/adversarial/frontend/worker/
  connector/Compose/load/chaos/screenshot/documentation checks pass with
  synthetic fixtures and retained evidence before production admission.

## Связанные материалы

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/03-module-boundaries.md`
- `docs/06-api.md`
- `docs/08-sync-reconciliation.md`
- `docs/23-notification-center.md`
- `docs/29-data-lineage.md`
- `docs/34-frontend.md`
- `docs/44-connector-conformance.md`
- `docs/46-sre-performance-slo.md`
- `docs/52-data-retention-archival.md`
- `docs/53-ai-agent-governance.md`
- `docs/69-frontend-shell.md`
- `docs/70-mcp-server.md`
- `adr/0008-mcp-openclaw.md`
- `adr/0021-ai-agent-guardrails.md`
- `adr/0095-enterprise-operations-ux-and-realtime-invalidation.md`
- `adr/0097-ai-provider-settings-and-openai-compatible-admission.md`
- `adr/0098-mcp-client-accounts-and-identity-resolver.md`
- `contracts/ai/agent-policy-v1.schema.json`
- `contracts/ai/agent-provenance-v1.schema.json`
- `contracts/ai/prompt-injection-regressions-v1.schema.json`
- `contracts/ai/tool-risk-policy.yaml`
- `contracts/mcp-tools.md`
- `tasks/issues/017-approval-engine.md`
- `tasks/issues/022-notifications.md`
- `tasks/issues/079-ai-agent-governance.md`
- `tasks/issues/122-ai-advisory-openai-compatible-connector.md`
- `tasks/issues/126-mcp-client-accounts.md`
- `tasks/issues/163-workflow-automation-builder.md`
- `tasks/issues/164-returns-cancellations-refunds.md`
- `tasks/issues/165-stock-forecast-auto-replenishment.md`
- `tasks/issues/166-product-publication-quality-center.md`
- `tasks/issues/167-channel-unit-economics.md`
- `tasks/issues/168-integration-state-center.md`
