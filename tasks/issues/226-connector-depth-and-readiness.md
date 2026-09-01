# Task 226 — Глубина интеграций: от манифеста до `ready`

## Статус

`planned` — в каталоге есть 61 манифест, но только 18 коннекторов имеют статус
`ready`; 17 фактически ограничены health-check. Наличие манифеста, SDK или
успешного ping не считается рабочей интеграцией.

## Цель

Сделать глубину каждого connector-а измеримой и довести приоритетные
интеграции до подтверждённого runtime-уровня:

```text
manifest → auth/health → read-only → partial operations → ready → qualified
```

Для каждого connector-а нужно отдельно показывать поддержанные capability,
ограничения, актуальность evidence и следующий шаг. Не требуется механически
поднять все 61 коннектора до `ready`: connector без подходящего business
surface должен остаться честным `separate_surface`, `health_only` или
`not_available`, а не имитировать глубину.

## Текущее состояние

| Уровень | Текущее значение | Требуемое действие |
|---|---:|---|
| Манифесты | 61 | провести инвентаризацию и назначить owner/приоритет |
| `ready` | 18 | подтвердить актуальным conformance/runtime evidence |
| `health_only` | 17 | либо добавить полезную operation surface, либо явно оставить health-only |
| Остальные поверхности | отдельные/частичные | описать capability depth, blockers и план квалификации |

Readiness должна считаться как по connector-у, так и по отдельной capability:
например, `orders.read` может быть `ready`, а `orders.status.write` —
`qualification_required`. Зелёный health-check одного аккаунта не поднимает
статус всех операций.

## Подзадачи

### 226.1 — Единая модель readiness и capability depth

- Зафиксировать состояния `manifest_only`, `health_only`, `read_only`,
  `partially_supported`, `ready`, `qualified`, `degraded`,
  `reauthorization_required` и `not_available`.
- Определить критерии перехода между состояниями и срок действия evidence.
- Считать readiness по capability, connector account и runtime operation, а
  агрегированный статус выводить по явному правилу, а не по наличию метода в
  SDK.
- Для каждой capability хранить auth scope, risk class, idempotency, timeout,
  retry, read-after-write, webhook/reconciliation и qualification source.
- Не считать `health_only` частично работающей бизнес-операцией.

**Acceptance:** одна и та же матрица readiness используется в manifest,
runtime registry, API, UI, worker и MCP; health-check не может сам перевести
connector в `ready`.

### 226.2 — Полный аудит 61 манифеста

**Зависимости:** 226.1.

- Собрать реестр всех connector ID, domain surface, capability, stage,
  `HealthOnly`, runtime adapter, account requirements и текущего evidence.
- Для каждой записи указать owner, приоритет, официальную документацию API,
  sandbox/test availability, дату последней проверки и blocker.
- Разделить `ready` на подтверждённые capability и лишние декларации, которые
  runtime фактически не допускает.
- Выявить манифесты без operation surface, дубли, устаревшие scope и
  connector-ы, которые нужно deprecated/retire вместо углубления.
- Сгенерировать machine-readable matrix и русское представление для
  Integration Center.

**Acceptance:** для всех 61 записей есть status, capability evidence, owner,
  next action и дата актуальности; ни один connector не остаётся без
  объяснимого статуса.

### 226.3 — Conformance suite как обязательный admission gate

**Зависимости:** 226.1–226.2.

- Расширить Task 064 suite проверками не только manifest/auth/health, но и
  каждой заявленной read/write capability.
- Проверять normalized errors, rate limits, timeout, idempotency, pagination,
  webhook signature, read-after-write, reconciliation и tenant boundaries.
- Для write operations обязательно проверять dry-run, approval/policy,
  unknown remote result и safe retry, если это поддерживает внешний API.
- Хранить redacted deterministic fixture, contract version, connector version,
  timestamp, environment и ссылку на evidence.
- Исключать из `ready` capability, для которой пройден только общий ping.

**Acceptance:** нельзя присвоить `ready`/`qualified`, пока обязательные checks
для заявленных операций не пройдены; пропущенный тест даёт fail-closed, а не
зелёный результат.

### 226.4 — План углубления 17 health-only connector-ов

**Зависимости:** 226.2–226.3.

- Для каждого из 17 connector-ов выбрать один из путей: business read,
  business write, отдельная специализированная поверхность, health-only с
  documented limitation или deprecation.
- Не обещать commerce operations connector-у, чей домен — AI, social,
  finance, government или notification; глубина должна соответствовать его
  назначению.
- Для выбранных приоритетных connector-ов определить минимальный vertical
  slice: account → sync/read → normalized model → operator action → status/
  reconciliation.
- Назначить owner, зависимости, официальные API/scopes, sandbox и дату
  qualification для каждого кандидата.
- Для оставшихся явно показывать причину, почему health-only является
  осознанным конечным статусом.

**Acceptance:** есть утверждённый план по всем 17 health-only connector-ам;
ни один не помечается `ready` только ради увеличения счётчика.

### 226.5 — Приоритетные commerce connectors: полезный read-срез

**Зависимости:** 226.2–226.4.

- Для выбранной первой волны marketplace/store connectors реализовать typed
  account, product, price, inventory, order, return и status readers согласно
  capability matrix.
- Поддержать cursor/checkpoint sync, remote ID mapping, immutable snapshots,
  deduplication, watermark и out-of-order handling.
- Нормализовать provider status/errors внутри adapter; raw payload и secrets не
  должны попадать в Core, Postgres, events, logs или UI.
- Добавить quality/freshness и reconciliation findings для partial, delayed,
  stale и contradictory facts.
- Не объявлять write capability на основании одного read-среза.

**Acceptance:** выбранный connector безопасно загружает synthetic и sandbox
данные, повтор sync не создаёт дубли, курсор восстанавливается после crash,
а отсутствующая операция отображается как `not_available`.

### 226.6 — Приоритетные write-срезы и сквозные операции

**Зависимости:** 226.5.

- Для каждой первой-wave capability реализовать только официальные typed
  writes: product/publication, price, inventory, order status, shipment,
  return/refund, promotion/advertising или payment — по domain ownership.
- Для каждого write задать idempotency identity, current-version check,
  approval/risk policy, dry-run, rate-limit budget и read-after-write.
- После timeout или remote acceptance сохранять `unknown` и reconciliation
  task; не повторять внешний write вслепую.
- Связать write operations с canonical Product/Offer/Order/Inventory/Payment,
  не создавать provider-specific ledger или вторую модель истины.
- Запретить операцию через API, UI, worker и MCP, если capability не прошла
  exact qualification.

**Acceptance:** для каждой включённой записи есть success, rejected, timeout,
duplicate, conflict, unknown и reconciliation tests; один retry не создаёт
двойной внешний эффект.

### 226.7 — Auth, account lifecycle и SecretProvider

**Зависимости:** 226.1–226.6.

- Проверять account/connector binding, scopes, tenant/workspace, reauthorization
  и срок действия credentials отдельным bounded flow.
- Хранить токены, passwords, certificates и private material только через
  scoped SecretProvider; не копировать их в манифест, audit, event или error.
- Добавить безопасную замену credential, disable, rollback и очистку runtime
  cache после отзыва.
- Различать `invalid_credentials`, `forbidden_scope`, `rate_limited`,
  `provider_unavailable`, `reauthorization_required` и `unknown`.
- Не считать успешный health с одним scope достаточным для другой capability.

**Acceptance:** negative tests на wrong tenant/account/scope, expired/revoked
secret, reauthorization и secret leakage проходят; health и business access
имеют раздельные permission checks.

### 226.8 — Durable sync, retries и reconciliation runtime

**Зависимости:** 226.5–226.7.

- У каждого admitted read/write operation должны быть checkpoint/watermark,
  inbox/outbox, lease fencing, retry budget, backoff/jitter и DLQ/manual
  attention route.
- Сохранять operation receipt, remote mapping, observed_at, effective_at,
  source quality и result без raw provider response.
- Поддержать повтор после process crash и безопасное продолжение после
  `accepted but response lost`.
- Развести `failed`, `unknown`, `partial`, `stale` и `reconciliation_required`;
  не маскировать их одним `degraded`.
- Обеспечить tenant-scoped retention, bounded queries и backpressure на
  массовых sync runs.

**Acceptance:** synthetic outage, duplicate webhook, out-of-order event,
worker crash, lease loss, rate limit и provider recovery восстанавливаются без
дублей и оставляют понятный audit/reconciliation trail.

### 226.9 — API, SDK, UI и документация статуса

**Зависимости:** 226.1–226.8.

- Добавить в Integration Center карточку connector-а с уровнем глубины,
  capability matrix, health отдельно от business operations, freshness,
  evidence и blocker/next action.
- Показывать для каждой операции русские статусы: «Только проверка связи»,
  «Только чтение», «Частично доступно», «Готово», «Нужна квалификация»,
  «Нет поддержки», «Требует переподключения».
- Добавить detail/history/retry/reconcile routes с tenant scope, cursor
  pagination, safe errors и correlation ID.
- Обновить OpenAPI, generated SDK, capability labels, connector docs и
  release report; технические стабильные codes сохранить.
- Не показывать кнопки записи, если runtime capability/policy/approval не
  подтверждены.

**Acceptance:** UI/API/SDK показывают одинаковые данные по всем 61 connector-ам;
health-only connector не получает ложных кнопок каталога, заказа, цены или
финансовой операции.

### 226.10 — Observability, supportability и стоимость эксплуатации

**Зависимости:** 226.8–226.9.

- Ввести метрики по connector/capability: sync lag, error/unknown rate,
  rate-limit, retry/DLQ, read-after-write mismatch, webhook lag, freshness,
  quota usage и qualification age.
- Добавить audit для изменения capability, статуса, credential, policy и
  ручного resolve; redact secrets, PII и raw payload.
- Добавить per-workspace/account quotas, concurrency limits, connector
  circuit-breaker/kill switch и отдельные budgets для reads/writes.
- Подготовить runbooks для reauthorization, stale data, provider outage,
  remote accepted/local timeout, mapping drift и rollback.
- Считать стоимость внешних вызовов и storage по connector-у, чтобы health
  polling не вытеснял бизнес-синхронизацию.

**Acceptance:** support-инженер по correlation ID может понять, почему
операция недоступна или зависла; kill switch останавливает side effects, но
не скрывает evidence и не повреждает canonical state.

### 226.11 — Qualification waves и release gate

**Зависимости:** все предыдущие подзадачи.

- Разбить connector-ы на волны: core commerce, logistics, finance, identity,
  notifications, AI/social и specialized surfaces; для каждой волны выбрать
  ограниченный набор бизнес-capabilities.
- Для каждого connector-а пройти deterministic tests, Docker conformance и
  официальный sandbox/live smoke с актуальными scopes.
- Для первого production-ready connector-а доказать не только account/health,
  но и полный выбранный vertical slice: read → normalize → operator action/
  write → webhook/status → reconciliation.
- В release report показывать counts `manifest`, `health_only`, `read_only`,
  `partial`, `ready`, `qualified`, а также список blockers; не оптимизировать
  KPI числом зелёных манифестов.
- Добавить регрессионный gate: новая capability не может понизить текущую
  безопасность, tenant isolation, contract compatibility или truthfulness
  статуса.

**Acceptance:** release report воспроизводим на exact topology; каждый
`ready`/`qualified` статус имеет retained evidence, а неподтверждённый
connector остаётся read-only/partial/health-only с причиной.

## Архитектурные ограничения

- Core не ветвится по именам провайдеров; различия живут в connector adapter,
  typed capability и mapping.
- PostgreSQL — источник операционной истины; события идут через transactional
  outbox, внешние deliveries защищены inbox/deduplication.
- Все операции tenant/workspace-scoped, idempotent и policy/approval-gated по
  risk class. Успешный ping не является разрешением на remote write.
- Официальные API предпочтительны; scraping, undocumented endpoints и обход
  rate limits запрещены.
- Секреты, access tokens, raw responses и лишний PII запрещены в коде, обычных
  SQL columns, events, audit, logs, fixtures и qualification evidence.

## Не входит в этот task

- Реализация новых бизнес-доменов, не нужных для выбранной qualification wave.
- Автоматическое превращение всех 61 манифестов в `ready`.
- Подмена отсутствующего официального API scraping-ом или browser automation.
- Production credentials и реальные персональные/клиентские данные в fixtures.

## Зависимости

025, 064, 074, 090, 164, 170, 217, 220, 221, 222, 223, 224, 225.

## Definition of Done

- Все 61 connector-а имеют честный readiness/capability profile, owner,
  evidence age и next action.
- Для каждой capability, показанной как `ready`/`qualified`, есть conformance,
  runtime, security, idempotency и reconciliation evidence.
- 17 health-only connector-ов имеют утверждённое решение: углубление,
  специализированная поверхность, health-only или deprecation.
- Integration Center, API, SDK, worker, MCP и release report не расходятся по
  статусам и доступным действиям.
- Пройдены `gofmt`, `go test ./...`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `make architecture`, `make migrations`,
  frontend typecheck/build и connector conformance на целевой topology.
