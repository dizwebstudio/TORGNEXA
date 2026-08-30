# Task 168 — Единый центр состояния интеграций

## Статус

`repository-complete` — реализованы типизированный reducer, tenant-scoped API,
OpenAPI/SDK, derived PostgreSQL metadata/queue, canonical events, worker
consumer и UI `/integrations/status`; release evidence и Compose smoke
проверяются общими репозиторными gate-командами.

## Цель

Собрать в одном tenant-scoped операторском центре достоверное состояние всех
интеграционных кабинетов и их рабочих контуров: runtime-admission, аккаунт,
credentials, non-secret configuration, capability grants, health/freshness,
rate limits, OAuth reauthorization, sync policies/jobs, reconciliation drift,
webhooks, отдельные поверхности AI/Delivery/Finance/CRM и доступные действия.

Центр должен отвечать на три разных вопроса, не смешивая их в один зелёный
badge:

1. **Подключён ли аккаунт и доступен ли транспорт?**
2. **Какие конкретные операции разрешены и действительно исполняются в
   production runtime?**
3. **Идёт ли обмен, свежи ли данные и есть ли расхождения/действия для
   оператора?**

Единый центр — это provider-neutral read model и очередь операторских действий,
а не новый источник истины для credentials, capabilities, sync, health,
reconciliation или connector runtime. Он не должен сам ходить к провайдерам при
обычном GET, включать возможности, менять политику синхронизации или выдавать
SDK/health-only коннектор за готовую бизнес-интеграцию.

## Текущий разрыв

- Каталог интеграций (`/integrations`) показывает карточки коннекторов и
  простое число здоровых кабинетов, но не связывает в одной строке account,
  runtime stage, capability, health history, rate limit, sync и drift.
- `GET /connector-accounts` возвращает account status и последнюю health
  запись; `:health-history` отдельно хранит ограниченную историю, а `:check`
  выполняет remote probe. Эти ответы не образуют атомарный центр состояния.
- `GET /sync/status` отдельно агрегирует policies/runs/drifts и может быть
  неполным для пользователя, у которого нет всех соответствующих permissions.
- runtime support contract, manifest, account capability grant и фактическая
  qualification — разные источники. Здоровый SDK/health-only аккаунт не
  означает, что его товарная, платежная, логистическая или CRM-операция
  исполняется.
- OAuth expiry/revocation, missing runtime config, rate-limit reset, retry/DLQ,
  reconciliation drift и last successful sync видны в разных местах и не
  имеют единой приоритизации причины.
- Dashboard считает только bounded operational counters; нет versioned
  snapshot/watermark, которое говорит, насколько свежа совокупная картина и
  из каких источников она собрана.

## Модель состояния

### Состояния источников

Каждый источник остаётся самостоятельным измерением. Допустимы только
нормализованные значения; raw provider status/text не проходит в public API.

| Измерение | Значения v1 | Источник истины |
|---|---|---|
| `runtime` | `ready`, `separate_surface`, `health_only`, `unsupported`, `not_registered`, `drifted` | generated runtime-support contract + host registry |
| `account` | `not_created`, `disabled`, `active`, `suspended`, `error` | connector account repository |
| `credential` | `missing`, `present`, `expired`, `reauthorization_required`, `invalid`, `unknown` | SecretProvider/OAuth health reason; bytes никогда не выдаются |
| `configuration` | `missing`, `invalid`, `valid`, `stale`, `unknown` | versioned non-secret runtime config |
| `health` | `unknown`, `healthy`, `degraded`, `unavailable`, `stale` | latest normalized health + health history |
| `capability` | `not_declared`, `declared`, `granted`, `enabled`, `blocked`, `qualification_required`, `stale` | manifest + account grant + runtime admission |
| `sync` | `not_configured`, `idle`, `running`, `retrying`, `failed`, `stale`, `paused` | sync policy/job/worker evidence |
| `reconciliation` | `not_configured`, `healthy`, `drift_open`, `failed`, `stale` | reconciliation runs/drifts |
| `webhook` | `not_configured`, `receiving`, `failing`, `stale`, `unsupported` | verified webhook/delivery evidence |
| `rate_limit` | `not_observed`, `available`, `limited`, `reset_unknown`, `stale` | normalized health/rate-limit snapshot |

`credential` — только безопасная классификация. Ни secret reference material,
ни access/refresh token, Authorization header, OAuth code/state, certificate
contents, provider URL с credential query и raw error не могут попасть в
center response, events, logs или screenshots.

### Итоговое состояние

`overall` вычисляется детерминированным reducer, но все исходные измерения и
`dominant_issue` всегда возвращаются рядом. Итоговое состояние не скрывает
вторичные проблемы.

Разрешённые значения: `healthy`, `attention`, `degraded`, `syncing`, `blocked`,
`setup_required`, `reauthorization_required`, `stale`, `disabled`,
`unsupported`, `unknown`.

Предлагаемый порядок доминирования (утвердить в ADR и зафиксировать тестами):

1. `unsupported`/`not_registered`, если операция не admitted runtime;
2. `blocked`, если capability/policy/approval/runtime gate запрещает действие;
3. `setup_required`, если нет аккаунта, credentials или обязательной
   non-secret configuration;
4. `reauthorization_required`, если OAuth/credential renewal требует участия;
5. `disabled`, если аккаунт отключён оператором;
6. `degraded`/`unavailable`, если transport/remote health нерабочий;
7. `stale`, если обязательная evidence старше TTL или источник неизвестен;
8. `attention`, если есть open drift, retry/DLQ, rate limit или failed run;
9. `syncing`, если допустимый обмен выполняется без ошибки;
10. `healthy` только если все обязательные для выбранной операции измерения
    валидны и свежи.

Если права пользователя не позволяют прочитать измерение, оно не считается
здоровым: возвращается `visibility=redacted`/`unknown`, а не зелёное значение.
`separate_surface` — это маршрут к отдельному рабочему экрану, не
`unsupported`; `health_only` — проверка доступа, но не domain execution.

## Freshness и evidence contract

Каждая проекция содержит:

- `observed_at` — когда source fact был зафиксирован;
- `checked_at` — когда health/worker/reconciliation probe завершился;
- `expires_at` или применённый TTL;
- `source_kind`, `source_ref`, `source_version` и bounded machine `reason_code`;
- `correlation_id`, `causation_id` при наличии;
- `evidence_digest` без payload-копии;
- `visibility` (`full`, `partial`, `redacted`);
- `stale_after_seconds` и вычисленный `age_seconds`.

Отсутствие `checked_at` не является здоровым `unknown`. Просроченная health,
sync или runtime evidence переводится в `stale`; старый healthy snapshot нельзя
использовать для включения capability или запуска remote write. TTL задаётся
по типу evidence и tenant policy, а не зашивается в frontend.

## Архитектурные границы

- PostgreSQL account/capability/config/sync/reconciliation/notification/audit
  repositories остаются authoritative. Derived center snapshot можно
  пересоздать из них и Kafka, но он не изменяет source rows.
- Kafka используется только через `EventBus`; новые status events проходят
  Transactional Outbox, consumers — Inbox/deduplication. ClickHouse/Valkey
  могут ускорять read/metrics, но не подтверждают account health, authorization
  или capability execution.
- Центр использует generated
  `contracts/connectors/builtin-runtime-support-v1.json` и manifest projection;
  provider names не участвуют в reducer или API handler.
- Remote health/check/reauthorization остаются отдельными idempotent actions.
  Обычный `GET` центра не читает SecretProvider и не выполняет network probe.
- Browser получает только same-origin API read model. OIDC bearer, tenant
  resolution, RBAC/ABAC и RLS остаются server-side; organization/workspace не
  принимаются из query/header/payload.
- Все actions учитывают текущий account version, capability grant, sync policy,
  approval and kill switch на момент выполнения; stale snapshot не является
  authorization.

## Подзадачи и порядок реализации

### 168.1 — ADR, scope и vocabulary

**Зависит от:** нет.

- Зафиксировать, что единый центр — read model/triage surface, а не новый
  domain owner и не универсальный incident ledger.
- Утвердить dimensions, statuses, overall reducer, dominance/secondary issue
  rules, visibility и terminology для русскоязычного UI/API.
- Разделить `runtime availability`, `account lifecycle`, `remote health`,
  `capability executable`, `sync freshness`, `reconciliation drift` и
  `operator action`.
- Определить обязательные evidence для `healthy` отдельно для generic
  product sync, AI, Finance, Delivery, CRM, Social и health-only surfaces.
- Согласовать compatibility with Tasks 010, 022, 063, 104–109, 120, 130, 134,
  156 and current runtime/worker composition; не переоткрывать закрытые
  connector tasks.

**Приёмка:** ADR содержит state machine/reducer table, examples для active
healthy, missing credentials, health-only, stale sync, OAuth reauth и
unsupported runtime; architecture review подтверждает отсутствие второго
источника истины.

### 168.2 — Canonical status/evidence contracts

**Зависит от:** 168.1.

- Создать provider-neutral structs/schema для `IntegrationStatusSnapshot`,
  `IntegrationAccountStatus`, `StatusDimension`, `EvidenceRef`, `Issue` и
  `OperatorAction`.
- Ограничить поля/размеры: bounded arrays, max issues, max reason length,
  canonical UTC timestamps, sortable IDs, no arbitrary `map[string]any` for
  stable fields.
- Ввести `snapshot_id`, `snapshot_version`, `generated_at`, `source_watermarks`,
  `partial`, `visibility`, `overall`, `dominant_issue` and dimension map.
- Разделить immutable evidence reference от mutable current projection;
  digest collision/version mismatch fails closed.
- Зарегистрировать Draft 2020-12 contract and compatibility policy; additive
  new dimension requires schema version when semantics change.

**Приёмка:** validator rejects raw secrets, provider payloads, unknown status,
non-UTC timestamps, excessive issue count, cross-tenant IDs and inconsistent
`healthy` without required evidence; fixtures cover every overall state.

### 168.3 — Source adapters и bulk read boundary

**Зависит от:** 168.2.

- Определить typed ports для bulk account, runtime support, capability,
  health-history, non-secret config, sync policy/job, reconciliation,
  webhook delivery, notification and audit-head reads.
- Сделать bulk/tenant-scoped queries instead of one request per card; preserve
  deterministic ordering and cursor pagination.
- Return source watermarks and query-time version for every adapter; partial
  dependency failure is recorded per dimension rather than turning all rows
  green or failing silently.
- Avoid direct imports from connector providers in Core; host composition may
  inject registry/runtime projections behind approved boundary.
- Add bounded consistency policy: either one DB transaction for compatible
  PostgreSQL sources or an explicit `snapshot_consistency=best_effort` with
  watermarks when sources cannot share a transaction.

**Приёмка:** one tenant with 100 accounts is served with bounded query count;
  injected source failures produce redacted partial evidence; no N+1 remote
  probes or SecretProvider reads occur on a center GET.

### 168.4 — Account lifecycle and credential/config state

**Зависит от:** 168.2–168.3, Tasks 105–106.

- Project account `disabled/active/suspended/error` plus missing/present
  credential classification from opaque secret reference and normalized health
  reasons.
- Map `oauth_reauthorization_required`, refresh failure, auth rejection,
  credentials missing/invalid and runtime-config validation to actionable
  reason codes with Russian safe labels.
- Distinguish account disabled by operator from disabled because activation
  gate failed; preserve version and last actor/audit link.
- Show non-secret runtime-config completeness/version without exposing values;
  config change invalidates dependent readiness snapshot.
- Never infer credential validity from a non-empty `secret_reference`; only a
  successful host-owned health/auth flow can produce healthy evidence.

**Приёмка:** account fixtures correctly classify no credentials, expired OAuth,
  reauthorization required, valid credentials with unavailable remote and
  manually disabled account; secret material is absent from JSON/logs.

### 168.5 — Runtime catalog and truthful admission projection

**Зависит от:** 168.2–168.3, Tasks 130/156.

- Join manifest metadata with generated runtime-support stage/surface,
  exact entity/direction/capability route, conformance report and qualification
  evidence.
- Represent `ready`, `separate_surface`, `health_only`, `unsupported`,
  `not_registered`, `drifted`; no legacy `planned` green fallback when contract
  has no executable record.
- Show separate surfaces (AI, Finance/FX, Delivery, CRM, Social) with a safe
  route/action, and generic product sync only where runtime bridge exists.
- Detect manifest/runtime/catalog drift and keep the card fail-closed until
  generated projections are regenerated and verified.
- Include connector package/runtime version and last qualification timestamp,
  but no credentials or raw conformance diagnostics.

**Приёмка:** all current catalog entries have a truthful stage; SDK-only and
health-only fixtures cannot expose domain actions; drifted generated files never
produce `healthy/executable`.

### 168.6 — Capability grant and operation readiness

**Зависит от:** 168.5, Task 107.

- Compare manifest declared capabilities, account-enabled subset, runtime
  support, current sync policy and approval/risk metadata.
- For each operation return `declared`, `granted`, `enabled`, `blocked`,
  `qualification_required` or `stale` plus a machine reason and safe action.
- Enforce default deny and do not treat an enabled UI checkbox as execution
  proof; worker/API recheck the current capability snapshot at action time.
- Summarize executable read/write directions separately; `products.read` must
  not imply `prices.write`, `orders.write`, `refund` or `shipment`.
- Show approval-required capabilities and pending approval without leaking
  approval payload or policy secrets.

**Приёмка:** capability matrix fixtures distinguish declared-but-not-granted,
granted-but-no-route, route-but-not-qualified and fully executable; no center
response authorizes an operation by itself.

### 168.7 — Health, freshness, rate-limit and failure normalization

**Зависит от:** 168.4–168.6, Task 109.

- Consume latest normalized health and bounded history categories:
  configuration, authentication, rate-limited, remote unavailable, degraded.
- Compute age/TTL/stale state and expose last successful check, last failure,
  next rate-limit reset and safe retry-after without provider-specific text.
- Preserve rate-limit remaining/reset only when connector reports normalized
  fields; unknown reset remains `reset_unknown`, not a guessed timestamp.
- Collapse repeated health failures into one issue with occurrence count and
  notification dedupe; severity may escalate but never downgrade silently.
- Keep operational health separate from authoritative audit and from generic
  `/api/v1/health` service liveness.

**Приёмка:** healthy evidence expires deterministically, rate-limit countdown
  never goes negative, repeated failures do not create notification storms, and
  raw provider error bodies never appear in center/history.

### 168.8 — Sync, worker, retry/DLQ and reconciliation dimensions

**Зависит от:** 168.3, 168.6–168.7, Tasks 013–014/108/113.

- Resolve account-specific policies, direction, source-of-truth, enabled state,
  latest run, retry age, DLQ age, last successful item and event lag.
- Map current sync state to `not_configured`, `idle`, `running`, `retrying`,
  `failed`, `stale`, `paused`; distinguish no policy from a healthy idle policy.
- Include reconciliation `open drift`, stale connector, failed run and pending
  remediation; do not mark a run complete from a dispatch row alone.
- Surface unsupported entity/direction and missing mapping as blocked issues with
  links to Sync/Reconciliation, not as remote outage.
- Bound source reads to current account/policy and preserve retry/DLQ machine
  codes; do not copy event payloads into the center.

**Приёмка:** fixtures cover first import, running worker, retry topic, DLQ,
  policy disabled, open drift, stale checkpoint and no-policy account; status
  agrees with `/sync/status` source records and never invents remote writes.

### 168.9 — OAuth, reauthorization and security posture links

**Зависит от:** 168.4, Tasks 106/134.

- Provide capability-gated actions for OAuth reauthorization, credential
  re-enrollment and runtime-config repair; action shows exact account/version
  precondition and expiry without token details.
- Reuse host-owned refresh runtime and distinguish routine access-token renewal
  from user-required authorization; browser redirects only through the existing
  PKCE boundary.
- Show certificate/key expiry class and quarantine/security hold only as safe
  statuses; private certificate material never crosses the center.
- Link to Settings/security/audit evidence instead of duplicating session,
  secret or provider account management.
- A reauthorization success does not broaden capabilities, enable sync or
  clear open drift automatically.

**Приёмка:** expired access tokens recover through host refresh without a false
  re-login; refresh failure becomes reauthorization-required; action replay,
  stale account version and cross-tenant account IDs fail closed.

### 168.10 — Notification, issue and operator-action model

**Зависит от:** 168.2, 168.7–168.9, Task 022.

- Define bounded issue types: setup, auth, config, runtime, capability,
  rate_limit, sync, retry, DLQ, drift, webhook, stale and qualification.
- Generate stable tenant/account/dimension/reason dedupe keys and map severity
  (`info`, `warning`, `critical`) to Notification Center; repeated observation
  increments occurrence without fan-out, escalation re-delivers.
- Define actions `check`, `reauthorize`, `configure`, `enable/disable account`,
  `open_sync`, `retry_safe`, `open_drift`, `view_history`; every action has
  permission, risk, idempotency and expected-version metadata.
- Keep read-only issue acknowledgement separate from source mutation; no
  “mark healthy” action.
- Include links to existing route/lineage/audit/approval evidence without
  putting secrets or raw payloads in URLs.

**Приёмка:** identical condition maps to one notification item and one issue;
  severity escalation is preserved; actions are denied when capability/approval
  is missing and issue acknowledgement cannot clear source health.

### 168.11 — Deterministic aggregate reducer and snapshot consistency

**Зависит от:** 168.2–168.10.

- Implement pure reducer from dimensions to `overall`, `dominant_issue`,
  `available_actions`, readiness counts and category summaries.
- Preserve all secondary issues and per-dimension evidence; never let a healthy
  transport hide a blocked write or open drift.
- Compute `healthy_accounts`, `attention_accounts`, `blocked_accounts`,
  `stale_accounts`, `unsupported_accounts`, `syncing_accounts` from the same
  snapshot rather than separate frontend counters.
- Add deterministic `snapshot_id` from tenant + source watermarks + policy
  versions; identical inputs produce identical digest.
- If sources change during read, return `partial=true`, source watermarks and
  `consistency=best_effort`; do not claim atomicity that was not obtained.

**Приёмка:** table-driven/property tests prove reducer precedence, no false
  green, stable digest and correct counters for all dimension combinations;
  concurrent account update yields stale/partial evidence rather than a torn
  executable status.

### 168.12 — PostgreSQL derived projection, RLS, lineage and retention

**Зависит от:** 168.2, 168.11.

- Decide in ADR which metadata is persisted: source watermark/run, snapshot
  digest, transition evidence, issue/action receipt and optional materialized
  current projection. None may replace source account/health/sync rows.
- Add expand-only migration only for bounded derived metadata and durable action
  receipts; all rows carry organization/workspace composite keys and FORCE RLS.
- Use optimistic versions, unique `(tenant, account, dimension, observed_at,
  digest)` and indexes for current/status/next-action queries; reject deletes of
  audit/transition evidence through application path.
- Link transitions to Task-003 audit, Task-030 lineage, source references and
  outbox event IDs; no full source payload copy.
- Apply Task-060/061 retention and legal holds: current projection may expire,
  security/financial/audit evidence follows its class and hold.

**Приёмка:** two-tenant RLS, duplicate transition, stale projection rebuild,
  migration upgrade/rollback and retention/legal-hold tests pass; `EXPLAIN`
  shows bounded tenant/current queries.

### 168.13 — Canonical events, realtime invalidation and durable worker

**Зависит от:** 168.11–168.12, Tasks 008/009/113/120.

- Register additive events such as
  `commerce.integration.account_status_changed.v1` and
  `commerce.integration.snapshot_published.v1` with canonical envelope,
  tenant, correlation/causation, dimension, status, digest and reason code.
- Publish only material transitions or coalesced snapshot changes; duplicate
  and out-of-order events are deduplicated through Inbox and never regress a
  newer status.
- Add durable scheduler/worker for recompute after account health, capability,
  sync, drift, webhook or runtime catalog change; coalesce by tenant/account
  and bound catch-up after outage.
- Reuse Task-120 SSE as metadata-only invalidation. Browser rereads the normal
  API; Kafka/event payloads, account IDs outside scope and issue bodies are not
  streamed to clients.
- Leases, retries, retry/DLQ and crash recovery follow existing worker runtime;
  CH/Valkey outage cannot make source account writes fail.

**Приёмка:** duplicate/out-of-order source events, worker crash before/after
  snapshot commit, lease loss, DLQ replay and realtime reconnect converge to one
  current digest; no invalidation storm exceeds configured debounce.

### 168.14 — REST/OpenAPI and permission-aware read API

**Зависит от:** 168.11–168.13.

- Add bounded `GET /api/v1/integration-center` with summary, cursor-paginated
  account rows, category/runtime counts, source watermarks, generated time,
  `partial`, `snapshot_digest` and supported filters.
- Add `GET /api/v1/integration-center/{account_id}` with dimensions, issue list,
  safe action descriptors, bounded transitions/history links and exact source
  timestamps. It performs no remote probe.
- Filters: family, connector/runtime surface, overall, health, sync, capability,
  issue, stale, account id and cursor; tenant/workspace selectors are forbidden.
- Define permission matrix: account/runtime/health fields require
  `connectors.accounts.read`; sync/drift fields require `sync.read`; audit/
  history links require `audit.read`; hidden fields return explicit redaction or
  are omitted by schema policy, never guessed healthy.
- Keep existing `:check`, OAuth, capabilities, sync and reconciliation APIs as
  the mutation/action owners. Any optional `:refresh` action must enqueue a
  bounded recompute, not call a provider synchronously.
- Add RFC7807 errors, ETags/conditional reads if useful, cursor limits,
  no-store headers for sensitive projections and OpenAPI/runtime route parity.

**Приёмка:** generated SDK and OpenAPI contain every production route; 401/403,
  field-level permission, cursor/range/unknown-filter and partial dependency
  cases are tested; response contains no secret/raw provider error and stays
  within response budget.

### 168.15 — UI «Единый центр состояния интеграций»

**Зависит от:** 168.14.

- Add a durable route (proposed `/integrations/status`) separate from the
  marketplace-style catalog; preserve deep-link account selection and browser
  back/forward behavior.
- Header summary: total accounts, healthy, attention, blocked, stale, syncing,
  unsupported/health-only and last snapshot time with `partial` warning.
- Main table/card layout: connector logo/name, surface/runtime stage, account
  lifecycle, health, executable capabilities, sync/reconciliation, last check,
  dominant issue and one safe next action. Provider card styling remains
  consistent with existing marketplace cards but state labels are factual.
- Filters/tabs for `Все`, `Требуют внимания`, `Работают`, `Синхронизация`,
  `Нужно настроить`, `Недоступны`, `В отдельных разделах`; filters are URL
  state, not local/session storage.
- Detail drawer/page shows a timeline by dimension, evidence age/TTL, rate-limit
  reset, capability matrix and links to Catalog/Sync/Approvals/AI/Delivery/
  Finance/CRM. Never show raw secret/config values.
- Use existing `DataTable`, status badges, skeleton/error/empty states,
  accessible labels, responsive layout, no overflow over action buttons and
  no large chart dependency without ADR.
- SSE invalidation invalidates query keys; it does not replicate status payloads
  or authorize a button. While hidden/unauthorized dimensions exist, explain
  “нет прав на раздел”, not “всё работает”.

**Приёмка:** browser/visual tests cover desktop/mobile, long connector names,
  100-account pagination, no-data/partial/error, stale/health-only/blocked
  cards, keyboard navigation, URL filters and no button overlap; Russian copy
  distinguishes “проверка подключения” from “операция доступна”.

### 168.16 — Idempotent operator actions and safe remediation

**Зависит от:** 168.10, 168.14–168.15.

- Wire actions to existing owners: check health, start OAuth, enroll
  credentials, save non-secret config, enable/disable account, run sync,
  open reconciliation, replay safe retry or open approval.
- Every mutation carries Idempotency-Key, account/snapshot expected version,
  actor, correlation and risk; stale center action returns conflict and asks
  for refresh.
- `check` and `reauthorize` never enable capabilities; enabling account requires
  current runtime support, config, capability, preview and healthy evidence.
- `retry_safe` cannot replay unknown external writes, refunds, payments or
  shipment effects; it routes to source worker/reconciliation policy.
- Dangerous actions require Task-017 approval and existing kill switches;
  center has no arbitrary command, SQL, HTTP, shell, browser or secret action.
- Record immutable action receipt/outcome and link it back to the issue; a
  successful action triggers recompute, not optimistic green UI.

**Приёмка:** duplicate clicks/retries produce one effect, stale version is
  rejected, permission/approval failures are explicit, action timeout remains
  unknown/pending rather than falsely successful, and source APIs remain the
  only mutation paths.

### 168.17 — Security, observability, SLO and quotas

**Зависит от:** 168.12–168.16.

- Threat-model cross-tenant enumeration, connector/account discovery,
  credential-status inference, OAuth phishing links, rate-limit disclosure,
  export/screenshot leakage and status spoofing from untrusted provider text.
- Enforce RLS, field-level capabilities, no-store, CSP/HTTPS/OIDC, bounded
  IDs/reason codes and safe link allowlists; never log tokens, headers, secret
  refs, raw bodies or customer PII.
- Metrics: snapshot latency/partial rate, source age, status transitions,
  stale/blocked/unknown counts, health-check failures, OAuth reauth rate,
  sync lag/retry/DLQ age, open drift, notification dedup, action conflicts and
  SSE invalidation rate.
- SLO targets must state dataset size/concurrency and small-VPS limits:
  bounded DB queries, max accounts/page, max issue/action entries, max refresh
  frequency, worker fan-out, memory, Kafka lag and API response bytes.
- Runbooks for provider outage, auth expiry, rate limit, stale worker, runtime
  catalog drift, migration failure, snapshot corruption, DLQ/replay and
  notification storm; recovery rebuilds from sources.
- Add tenant kill switch for recompute/realtime fan-out without blocking
  ordinary account or commerce writes.

**Приёмка:** security tests cannot infer another tenant’s account state;
  load/chaos tests keep quotas bounded; dashboards/alerts contain actionable
  machine codes and links to existing operational runbooks.

### 168.18 — Tests, Compose, demo data и документация

**Зависит от:** 168.1–168.17.

- Unit/property tests for every dimension validator, reducer precedence,
  freshness/TTL, counter conservation, redaction, visibility and action policy.
- Contract tests for account/status/event/OpenAPI schemas, generated SDK,
  runtime-support/catalog drift and route parity.
- PostgreSQL integration/RLS/migration/lineage/retention tests; source-adapter
  bulk query and partial-failure tests; Inbox/Outbox/worker lease/retry/DLQ
  tests; notification dedup/escalation tests.
- API tests for tenant scope, permissions, cursors, ETags/partial snapshots,
  no-remote-IO GET, stale action conflict and idempotent action replay.
- Compose E2E: create synthetic accounts for a ready marketplace/storefront,
  health-only delivery, separate AI/FX surface and unsupported capability;
  exercise credentials missing, OAuth reauth, rate limit, sync retry, DLQ,
  open drift, worker restart, source outage and SSE invalidation.
- Seed only synthetic data; screenshots must show healthy, blocked, stale,
  health-only, separate-surface and partially redacted states without secret
  values. Document exact startup, fixtures, state meanings and troubleshooting.
- Run `gofmt -w` for changed Go files, `go test ./...`, `go vet ./...`,
  contract/architecture/migration/frontend checks, Compose smoke and bounded
  performance; retain snapshot/event/action IDs and dataset manifest.

**Приёмка:** tests prove no false green state, no cross-tenant data, no remote
  call on read, deterministic replay and safe recovery; documentation and
  screenshots match the shipped UI and every release gate is green.

## Порядок поставки

1. **Foundation:** 168.1–168.5 — ADR, contracts, source adapters, account and
   truthful runtime projection.
2. **State engine:** 168.6–168.11 — capabilities, health, sync/drift,
   reauthorization, issues and deterministic reducer.
3. **Durability/runtime:** 168.12–168.14 — PostgreSQL derived metadata, events,
   worker, realtime and read API.
4. **Operator workflow:** 168.15–168.17 — UI, safe actions, permissions,
   observability and quotas.
5. **Release:** 168.18 — tests, Compose, demo, screenshots, docs and evidence.

## Явно исключено из Task 168

- хранение или показ access/refresh tokens, OAuth codes, private keys,
  credentials, raw provider responses, Authorization headers и PII;
- синхронные remote probes, автоматическое включение account/capability,
  изменение sync policy, reconciliation auto-fix или retry неизвестного
  внешнего write из обычного GET/UI read;
- новый глобальный incident ledger, второй health/secret/sync source of truth,
  Kafka/ClickHouse/Valkey как authoritative status store;
- provider-specific ветвления в Core, browser scraping, arbitrary HTTP/SQL/
  shell/code actions и UI-фильтры, которые обходят API permissions;
- объявление SDK-only, health-only, separate-surface или unqualified runtime
  коннектора полноценной commerce-операцией;
- стирание health/audit/transition history, downgrade severity или silent
  clearing of an issue;
- изменение Product/Order/Payment/Inventory/Settlement facts и исполнение
  платежей, refunds, shipment, publication или рекламы из центра.

## Gate RUNTIME-168

- единая read model отображает account, runtime, credentials/config class,
  capability, health/freshness/rate-limit, sync/retry/DLQ, reconciliation,
  webhook и separate-surface dimensions без потери исходных состояний;
- `healthy` выдаётся только при свежих подтверждённых обязательных evidence;
  `health_only`, `separate_surface`, `unsupported`, `stale`, `blocked` и
  `unknown` никогда не маскируются зелёным статусом;
- каждый row/snapshot tenant-scoped, versioned, digest-bound, permission-aware
  и содержит source watermarks, age/TTL, reason code, visibility и safe next
  action; no secrets/raw errors/PII;
- runtime stage берётся из generated manifest/runtime-support/qualification,
  а executable capability — из текущего account grant и host route; manifest
  alone не authorizes operations;
- GET center performs no remote IO and cannot mutate source state; all checks,
  OAuth, sync, remediation and approval actions reuse existing idempotent API/
  worker/connector boundaries;
- status transitions, notifications, Outbox/Inbox, worker/realtime invalidation
  and retries are duplicate/out-of-order/crash/lease/DLQ safe; source outage
  produces partial/stale evidence rather than false health;
- API/OpenAPI/SDK and UI preserve tenant permissions, cursor/response limits,
  URL filters, accessibility, responsive layout and exact Russian status copy;
- derived snapshot rebuild, PostgreSQL RLS, lineage, retention/legal hold,
  migration/backup and source watermark checks pass;
- small-VPS quotas bound database queries, refreshes, event fan-out, memory,
  Kafka lag and SSE invalidations; alerts and runbooks cover each dominant issue;
- Go, contract, architecture, migration, frontend, connector/conformance,
  Compose, performance, screenshots, documentation and release evidence pass
  before production admission.

## Связанные материалы

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/03-module-boundaries.md`
- `docs/04-event-platform-kafka.md`
- `docs/05-database.md`
- `docs/06-api.md`
- `docs/08-sync-reconciliation.md`
- `docs/10-integrations-matrix.md`
- `docs/23-notification-center.md`
- `docs/29-data-lineage.md`
- `docs/34-frontend.md`
- `docs/44-connector-conformance.md`
- `docs/46-sre-performance-slo.md`
- `docs/52-data-retention-archival.md`
- `docs/69-frontend-shell.md`
- `adr/0009-transactional-outbox.md`
- `adr/0010-connector-capabilities.md`
- `adr/0016-connector-conformance.md`
- `adr/0095-enterprise-operations-ux-and-realtime-invalidation.md`
- `adr/0100-runtime-truthful-integration-catalog.md`
- `tasks/issues/022-notifications.md`
- `tasks/issues/104-integration-catalog-settings.md`
- `tasks/issues/105-connector-account-settings.md`
- `tasks/issues/106-connector-authentication-flow.md`
- `tasks/issues/107-connector-capability-settings.md`
- `tasks/issues/108-connector-bootstrap-sync.md`
- `tasks/issues/109-connector-health-settings.md`
- `tasks/issues/120-enterprise-operations-ux.md`
- `tasks/issues/130-runtime-truthful-integration-catalog.md`
- `tasks/issues/134-host-owned-oauth-refresh-runtime.md`
- `tasks/issues/156-categorical-runtime-surfaces.md`
