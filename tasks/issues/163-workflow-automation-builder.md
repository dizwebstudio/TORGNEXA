# Task 163 — Конструктор автоматизаций

## Status

`planned` — декомпозиция подготовлена, реализация не начата.

## Objective

Дать оператору визуальный и API-first конструктор автоматизаций: событие или
расписание запускает проверяемую цепочку условий и действий, а результат
остаётся наблюдаемым, идемпотентным, tenant-scoped и подчинённым обычным
capability/policy/approval правилам.

Конструктор не должен превращаться в произвольный исполняемый код,
провайдерский скрипт или второй скрытый планировщик. Он использует существующие
EventBus/Transactional Outbox/Inbox, PostgreSQL scheduler, worker, уведомления,
согласования, аудит, lineage и connector ports.

## Architecture boundaries

- Core остаётся provider-neutral: workflow action ссылается на capability и
  типизированный application port, а не на `ozon`, `telegram` или другой
  provider id.
- PostgreSQL — источник истины для workflow definition, immutable versions,
  run/step state, leases и evidence; Kafka передаёт события, но не хранит
  состояние расписаний или прогресса.
- Каждое внешнее side effect выполняется через существующий host-mediated
  connector boundary, с idempotency key, timeout, retry classification и
  read-after-write/reconciliation там, где это поддерживает провайдер.
- `write_sensitive` и `legally_significant` actions проходят Task-017 approval;
  AI, MCP, n8n и сам workflow engine не получают обходной authority.
- События и логи хранят только opaque references, версии, hashes и bounded
  reason codes. Raw provider payloads, credentials, Authorization headers и
  произвольный пользовательский PII в workflow state не попадают.
- Любые циклы, fan-out, payload, глубина и длительность исполнения ограничены
  server-side; нет unbounded loop, recursive workflow или dynamic code execution.

## Subtasks and implementation order

### 163.1 — ADR, scope and action catalog

**Depends on:** none.

- Зафиксировать ADR для workflow automation и границы первой версии.
- Утвердить provider-neutral vocabulary: trigger, condition, action, delay,
  approval, run, step, version и execution outcome.
- Составить allowlist первых триггеров и действий из уже существующих портов:
  order/product/inventory/connector-health events, schedule, notification,
  sync/reconciliation, approval request и bounded safe domain commands.
- Для каждого action указать risk class, required capability, input/output
  schema, idempotency semantics, timeout, retryability и dry-run support.
- Отдельно перечислить действия, которые в первой версии запрещены: arbitrary
  HTTP, SQL, shell/plugin code, credentials, private keys, irreversible
  payment/refund/regulated writes без approval.

**Acceptance:** ADR, action catalog и scope review одобрены; ни один action не
может быть объявлен исполняемым только наличием manifest capability.

### 163.2 — Canonical workflow model and version lifecycle

**Depends on:** 163.1.

- Ввести provider-neutral модели `Workflow`, `WorkflowVersion`, `Trigger`,
  `Node`, `Edge`, `Run`, `StepRun` и `ExecutionEvidence`.
- Зафиксировать состояния definition: `draft -> published -> paused ->
  archived`; опубликованная версия immutable.
- Зафиксировать состояния run/step, optimistic version и terminal semantics:
  `queued -> running -> waiting_approval|waiting_retry|completed|failed|cancelled`.
- Определить variable binding: только typed event fields и безопасные
  references; secret values и произвольный eval context запрещены.
- Предусмотреть ручной cancel/resume/replay только с новой run identity и
  сохранением исходной evidence.

**Acceptance:** typed domain package валидирует lifecycle, запрещает backward
transitions и не содержит connector/provider branches.

### 163.3 — Contract/DSL schema and deterministic compiler

**Depends on:** 163.1, 163.2.

- Добавить Draft 2020-12 schema для workflow definition и versioned contract
  для run/step evidence.
- Реализовать строгий parser/validator: неизвестные поля отклоняются,
  references типизированы, граф acyclic, все nodes достижимы, trigger/action
  существуют в allowlist.
- Добавить bounded limits: максимум nodes/edges, глубина, fan-out, размер
  definition/input/output, число переменных и общий execution deadline.
- Компилировать definition в deterministic execution plan с digest; одинаковая
  версия всегда даёт одинаковый plan hash.
- Не выполнять expressions через Go `eval`, shell, reflection-based arbitrary
  calls или загрузку кода из workflow payload.

**Acceptance:** positive/negative schema fixtures покрывают циклы, неизвестные
actions, type mismatch, oversized graph, secret-shaped fields и cross-tenant
references; compiler выдаёт стабильный digest.

### 163.4 — PostgreSQL persistence and tenant/RLS migration

**Depends on:** 163.2, 163.3.

- Добавить expand migration для workflows, immutable versions, runs, step runs,
  leases, deduplication receipts и bounded execution evidence.
- На всех tenant-owned relations включить forced RLS, composite tenant keys,
  optimistic versions и append-only guards для historical evidence.
- Добавить индексы для due schedules, active versions, `(tenant, trigger,
  event_id)` dedup и operator run queries; проверить планы на bounded pages.
- Не хранить полный входной event/payload: сохранять только canonical digest,
  typed references, counters и machine error codes.
- Добавить retention policy для завершённых runs и безопасное archival без
  удаления approval/audit/lineage evidence раньше установленного срока.

**Acceptance:** migration static, fresh install, upgrade/rollback rehearsal,
двухтенантный RLS smoke и append-only negative tests проходят.

### 163.5 — Event triggers, schedules and durable dispatch

**Depends on:** 163.3, 163.4.

- Добавить consumer для разрешённых canonical EventBus event types через Inbox;
  повторная доставка одного `event_id` создаёт не более одного logical run.
- Добавить PostgreSQL-owned time trigger state и scheduler lease по существующему
  Task-108 pattern; Kafka не используется как delayed-job store.
- Поддержать debounce/coalescing для одинакового trigger key и bounded catch-up
  после простоя, без массового burst на маленькой VPS.
- При старте run повторно проверить tenant scope, workflow version, account
  status, capability, policy и approval requirements.
- Для каждого run/step использовать deterministic idempotency key и fenced
  lease; stale worker не может подтвердить чужой lease.

**Acceptance:** event replay, duplicate delivery, scheduler restart, lease loss,
clock skew в допустимом диапазоне и bounded catch-up не создают duplicate runs
или cross-tenant work.

### 163.6 — Execution engine, conditions, retries and approvals

**Depends on:** 163.2–163.5.

- Реализовать bounded state machine для sequential/conditional/parallel-safe
  steps; parallelism ограничить per-workspace и global worker budgets.
- Условия вычислять над typed snapshot без network calls и side effects.
- Retry только для retry-safe classifications с jitter/backoff; permanent,
  invalid и ambiguous outcomes переходят в terminal/manual attention.
- При `approval_required` создать обычный Task-017 request с exact workflow
  version, run, step, action digest и ресурсом; после approval повторно
  revalidate все authority checks.
- Уметь pause/cancel/resume/retry-one-step без переписывания исторической
  evidence и без повторной выдачи уже подтверждённого внешнего side effect.

**Acceptance:** crash before/after action, duplicate retry, approval expiry,
rejection, cancellation и unknown remote outcome покрыты тестами; side effect
не выполняется при отсутствии policy/approval.

### 163.7 — Action adapters and safe first vertical slice

**Depends on:** 163.1, 163.6.

- Ввести registry typed action handlers, не импортирующий provider packages в
  Core и не дающий workflow payload прямого доступа к SQL/HTTP/secrets.
- Первую вертикаль ограничить безопасными действиями с существующими портами:
  создать уведомление, запустить reconciliation, запросить approval и
  выполнить dry-run/metadata-only действие.
- Затем отдельно добавить один reversible commerce action после его
  capability/policy/idempotency/reconciliation review.
- Все connector actions передавать через host runtime, `SecretProvider.Use`,
  timeout, rate-limit budget и normalized error mapping.

**Acceptance:** end-to-end synthetic workflow выполняет trigger → condition →
action → evidence; provider-specific action без зарегистрированного adapter
отклоняется до remote call.

### 163.8 — REST/OpenAPI and operator UI

**Depends on:** 163.2, 163.3, 163.6.

- Добавить tenant-scoped API для списка, создания draft, validate, publish,
  pause/archive, ручного test-run, run list/detail и bounded retry/cancel.
- Все mutations требуют idempotency key и optimistic version; API не принимает
  tenant/workspace из доверенного payload.
- Обновить OpenAPI и все generated SDK после стабилизации контрактов.
- Добавить UI «Автоматизации»: список/статус, пошаговый builder, визуальный
  validation error, preview/dry-run, publish confirmation, run timeline,
  retry/manual attention и ссылку на approval/audit evidence.
- До публикации явно показать required capability, risk, лимиты и отсутствие
  поддержки для недоступного connector action.

**Acceptance:** оператор может создать draft, получить понятную ошибку
валидации, опубликовать версию, запустить synthetic test-run и увидеть каждый
step outcome; UI не показывает fictitious provider capability.

### 163.9 — Observability, quotas and operator recovery

**Depends on:** 163.5–163.8.

- Метрики: trigger lag, queued/running runs, step latency, retry age, approval
  wait age, failure/DLQ rate, fan-out, per-tenant concurrency и saturation.
- Structured logs только с workflow/version/run/step IDs, correlation/causation
  и machine error codes; raw event/provider text redacted.
- Установить per-workspace quotas: active workflows, runs/minute, concurrent
  runs, max action calls и retention budget.
- Добавить operator recovery runbook: pause workflow, drain/retry, inspect
  evidence, replay from a new run id и безопасно disable action/version.

**Acceptance:** quota breach is deterministic and tenant-local; dashboards and
alerts distinguish waiting approval, retryable outage, permanent failure and
manual intervention without leaking payloads.

### 163.10 — Qualification, performance and documentation

**Depends on:** all previous subtasks.

- Добавить Go unit/integration tests, contract fixtures, RLS tests, API tests,
  frontend tests and deterministic Docker smoke with synthetic events.
- Провести load profile for event burst, scheduler catch-up, fan-out and retry;
  confirm bounded memory/DB connections on the small-VPS Compose profile.
- Добавить chaos cases: Kafka redelivery, PostgreSQL restart, worker crash at
  each side-effect boundary, approval timeout and connector rate limiting.
- Обновить product scope, architecture, API/event docs, operations/runbook,
  public frontend documentation and `.env` reference.
- Сформировать retained qualification evidence; не переводить feature в
  production-ready до прохождения exact deployment topology gates.

**Acceptance:** `go test ./...`, `go vet ./...`, contracts, architecture,
migrations, frontend, conformance, performance and Compose E2E checks pass;
documentation matches the implemented action catalog and runtime support.

## Suggested delivery slices

1. **Foundation:** 163.1–163.4 — contracts, model, compiler and durable schema.
2. **Safe automation MVP:** 163.5–163.7 — event/schedule triggers, notification,
   reconciliation, dry-run and approval actions.
3. **Operator product:** 163.8–163.9 — API, builder, run monitor, quotas and
   recovery controls.
4. **Release qualification:** 163.10 — load/chaos/e2e, docs and evidence.

## Explicit exclusions

- arbitrary user code, shell, SQL, browser automation or unbounded HTTP steps;
- provider-specific branches in Core or a second provider scheduler;
- automatic execution of sensitive AI/MCP/n8n requests without normal policy,
  approval, audit and idempotency checks;
- storing secrets, raw credentials, full event payloads or private signing keys
  in workflow definitions, runs, events, logs or browser storage;
- claiming domain capability for a connector merely because its SDK manifest
  advertises the operation.

## References

- `docs/01-architecture.md`
- `docs/03-module-boundaries.md`
- `docs/04-event-platform-kafka.md`
- `docs/08-sync-reconciliation.md`
- `docs/14-observability.md`
- `docs/24-workflow-approval.md`
- `docs/46-sre-performance-slo.md`
- `adr/0009-transactional-outbox.md`
- `adr/0018-slo-performance.md`
- `adr/0041-approval-engine-policy-evidence.md`
- `adr/0056-replenishment-planning.md`
- `adr/0095-enterprise-operations-ux-and-realtime-invalidation.md`
