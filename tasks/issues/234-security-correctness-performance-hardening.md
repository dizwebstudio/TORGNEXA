# Task 234 — Security correctness и performance hardening

## Статус

`planned` — follow-up по результатам статического security/performance-аудита
от 2026-09-04.

```yaml
repository_status: backlog
release_blockers: [234.1, 234.2, 234.3]
security_priority: high
external_evidence_required: false
```

## Цель

Устранить подтверждённые гонки и разрывы атомарности в IAM/payment/audit,
восстановить независимую проверку topic входящих commerce webhook и убрать
узкие места OAuth, OIDC, SSE и rate limiting. Изменения сохраняют modular
monolith, PostgreSQL как system of record, Transactional Outbox, forced RLS,
capability-based connector boundary и запрет на plaintext credentials.

## Порядок выполнения

| Подзадача | Приоритет | Результат | Зависимости |
| --- | --- | --- | --- |
| 234.1 | P0 | Нельзя конкурентно удалить последнего активного admin | tenancy repository, PostgreSQL integration tests |
| 234.2 | P0 | Payment receipt и применение verified status не расходятся | ADR-0071, ADR-0105, payments repository |
| 234.3 | P0 | Привилегированная mutation не коммитится без audit evidence | Task 003 audit, settings repositories |
| 234.4 | P1 | Commerce webhook topic привязан к серверной подписке | commerce webhook SDK/runtime, OpenAPI |
| 234.5 | P1 | OAuth refresh не блокирует собственный DB pool | ADR-0104, SecretProvider |
| 234.6 | P1 | Auth hot path не делает повторную membership resolution | ADR-0083, OIDC/session stores |
| 234.7 | P1 | SSE живёт дольше общего WriteTimeout и масштабируется по tenant | ADR-0095, realtime API |
| 234.8 | P2 | Rate limit согласован между репликами и не сериализует весь процесс | security edge, Valkey adapter |
| 234.9 | P2 | Внешний API/UI не раскрывает внутренний OIDC subject reference | privacy classification, OpenAPI |
| 234.10 | P1 | Regression, failure-injection, SAST и load gates ловят эти классы дефектов | все предыдущие подзадачи |

## Подзадачи

### 234.1 — Сериализовать invariant последнего администратора

- [ ] Перед count/update сериализовать изменения admin-состава на уровне
  `(organization_id, workspace_id)`: tenant-scoped advisory transaction lock,
  блокировка полного набора активных admin в детерминированном порядке либо
  `SERIALIZABLE` с ограниченным retry.
- [ ] Не считать блокировку только изменяемой строки достаточной защитой.
- [ ] Сохранить optimistic `expected_version` и default-deny authorization.
- [ ] Привязать `Idempotency-Key` к digest нормализованного payload; повтор с
  тем же ключом и другим role/status должен завершаться конфликтом.
- [ ] Добавить PostgreSQL-тест: два параллельных запроса отключают двух разных
  admin; ровно один коммитится, после завершения остаётся один active admin.
- [ ] Добавить тесты concurrent demotion, disable и корректного replay одного
  и того же payload.

### 234.2 — Сделать verified payment webhook атомарным и повторяемым

- [ ] После удалённой проверки передавать нормализованный verified result в
  один repository boundary, который атомарно фиксирует receipt, меняет payment,
  добавляет audit и Transactional Outbox event.
- [ ] Не переводить receipt в terminal/applied до успешного status transition.
  Если выбран `pending/applied/rejected` state machine, pending должен иметь
  lease/retry/DLQ и наблюдаемую причину остановки.
- [ ] После доказанной подлинности внутренняя ошибка должна либо дать провайдеру
  retryable ответ, либо быть надёжно поставлена в durable retry до `2xx`.
  До verification сохранить одинаковый ответ без account enumeration.
- [ ] Повтор уже применённой доставки остаётся no-op и не создаёт повторных
  audit/outbox side effects.
- [ ] Добавить failure-injection тесты: receipt insert success + transition
  failure, optimistic conflict, DB outage, redelivery и worker recovery.
- [ ] Подтвердить, что reconciliation остаётся safety net, а не единственным
  способом восстановить потерянную verified delivery.

### 234.3 — Объединить привилегированные settings mutations с аудитом

- [ ] Инвентаризировать member, workspace, profile, identity-provider,
  security и connector-account writes, где `audit.Capture` вызывается после
  уже закоммиченной mutation.
- [ ] Для `write_sensitive` и `legally_significant` путей писать authoritative
  audit record или durable audit intent в той же PostgreSQL-транзакции.
- [ ] Ошибка обязательного аудита должна откатывать business mutation; retry с
  тем же idempotency key не должен создавать дубликаты.
- [ ] Audit summary остаётся bounded/redacted и не получает email, raw OIDC
  subject, credentials или provider payload.
- [ ] Добавить тесты с injected audit failure для role/status, identity-provider
  enable/disable и profile update.

### 234.4 — Привязать commerce webhook topic к доверенному ожиданию

- [ ] Перестать формировать `ExpectedTopic` из того же HTTP-заголовка, который
  заполняет `HeaderTopic`.
- [ ] Для провайдеров, у которых topic не входит в подпись body, выдавать
  отдельный непредсказуемый subscription endpoint/reference, серверно
  связанный с account и exact expected topic.
- [ ] Сравнивать provider header с серверной subscription configuration до
  dedup/outbox claim; для подписанного event в body дополнительно проверять
  семантическое совпадение внутри connector adapter.
- [ ] Delivery fingerprint не должен позволять replay одного подписанного body
  сначала зарегистрировать под ложным topic и поглотить корректную доставку.
- [ ] Обновить OpenAPI, migration/compatibility notes и connector conformance
  fixtures для WooCommerce и Saleor.

### 234.5 — Убрать nested-transaction deadlock из OAuth refresh

- [ ] Advisory lock, повторное чтение bundle и encrypted rotation должны
  использовать один явно переданный SQL transaction/connection boundary либо
  отдельный coordinator pool с гарантированным резервом connections.
- [ ] Не допускать схему, где все connections удерживают outer lock-транзакции
  и одновременно ждут nested `SecretProvider.Use/Rotate`.
- [ ] Сохранить distributed serialization между API/worker и не выполнять два
  remote refresh для rotating refresh token.
- [ ] Добавить bounded concurrency, jittered backoff и метрики lock wait,
  refresh latency/failure и pool saturation.
- [ ] Добавить тесты для `MaxOpenConns=1`, pool-size concurrent accounts,
  timeout/cancel, rejected refresh и rotated-token replay.
- [ ] Если минимальный размер пула временно повышается, валидировать его при
  startup и задокументировать как mitigation, а не окончательное исправление.

### 234.6 — Сократить OIDC/session/membership hot path

- [ ] Проверять подпись JWT локально через issuer-bound JWKS cache с rotation;
  валидировать issuer, audience/authorized party, expiry/not-before и subject.
  Неподписанный decoded payload не является authorization evidence.
- [ ] Убрать обязательный UserInfo HTTP round-trip с каждого API-запроса;
  использовать его для bounded profile hydration/refresh, а не для каждой
  проверки доступа.
- [ ] Разрешать membership один раз и передавать database-authoritative member
  в authorizer через typed request context.
- [ ] Проверку revoked session сохранить fail-closed, но обновление
  `last_seen_at` coalesce/throttle, чтобы активный пользователь не создавал
  запись в PostgreSQL на каждый запрос.
- [ ] Добавить метрики количества IdP/DB calls на запрос и authenticated load
  profile с p50/p95/p99, Keycloak outage и revoked-session сценариями.

### 234.7 — Исправить lifecycle и fan-out realtime SSE

- [ ] Для SSE явно снять или продлевать write deadline после прохождения
  обычной auth/tenant/authz композиции; общие HTTP timeouts для остальных
  маршрутов не ослаблять.
- [ ] Добавить integration-тест с реальным `http.Server`: stream переживает
  configured `WriteTimeout`, получает heartbeat и завершается по cancel.
- [ ] Заменить polling audit head каждые две секунды на каждого клиента одним
  tenant-scoped watcher/broadcaster или эквивалентным multiplexing. Durable
  event/outbox остаётся источником сигнала; SSE payload остаётся metadata-only.
- [ ] Ограничить clients per tenant/process, bounded buffers и slow-consumer
  поведение; не удерживать неограниченную историю в памяти.
- [ ] Добавить reconnect-storm и multi-client load profile с DB query count.

### 234.8 — Подготовить rate limiter к нескольким репликам

- [ ] Ввести интерфейс limiter и распределённую Valkey-реализацию для
  multi-replica deployment; in-memory вариант разрешать только для явно
  single-node/development topology.
- [ ] Разделить pre-auth IP budget, authenticated tenant/principal budget и
  public webhook budget без attacker-controlled unbounded key cardinality.
- [ ] Убрать единый global mutex/O(n) sweep из request hot path: shard/expiry
  queue/background cleanup для локального fallback.
- [ ] Зафиксировать fail-open/fail-closed поведение при недоступности Valkey,
  `Retry-After`, метрики и alerting.
- [ ] Добавить тесты shared NAT, distributed IPs, replica multiplication,
  cardinality exhaustion и concurrent access.

### 234.9 — Минимизировать OIDC subject reference в API/UI

- [ ] Убрать отображение `oidc_subject` из member UI и перестать возвращать
  внутренний stable reference в обычном member response.
- [ ] Если UI нужен статус привязки, вернуть неперсональный boolean
  `identity_bound`; провести совместимое изменение OpenAPI/SDK по действующей
  compatibility policy.
- [ ] Сохранить OIDC reference только внутри trusted application/privacy
  boundaries и проверить export/retention/delete semantics.
- [ ] Добавить contract/API тест, запрещающий internal identity reference в
  member response и audit/event payload.

### 234.10 — Закрепить security и performance regression gates

- [ ] Добавить PostgreSQL failure/concurrency suite для 234.1–234.5; unit fakes
  не заменяют проверку isolation/commit semantics.
- [ ] Расширить production qualification authenticated request mix и множеством
  SSE clients; один `/health` burst не считается покрытием auth hot path.
- [ ] Разобрать текущий gosec baseline: исправить реальные находки, устранить
  двойное сканирование и оставить только узкие `#nosec RULE -- justification`
  для доказанно безопасных casts, synthetic fixtures и provider-required
  legacy crypto.
- [ ] `govulncheck` запускать для root и всех вложенных Go modules; Trivy secret
  scan должен отличать synthetic fixtures от настоящих credentials.
- [ ] Сохранить redacted отчёт с commit SHA, scanner versions, p50/p95/p99,
  DB/IdP call counts, pool saturation и injected-failure results.

## Definition of Done

- 234.1–234.3 закрыты до release candidate; подтверждённые P0 сценарии имеют
  PostgreSQL regression tests и не зависят только от reconciliation/manual
  recovery.
- 234.4–234.9 реализованы с нужными ADR/contract/migration/privacy updates;
  security boundary не ослаблена ради производительности.
- Все новые durable writes tenant-scoped, idempotent и совместимы с forced RLS.
- Секреты, raw webhook bodies, OIDC subject и Authorization headers не
  появляются в logs, audit, events, fixtures или plaintext columns.
- Выполнены `gofmt`, `go test ./...`, targeted `go test -race`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `./scripts/check-architecture.sh`, migration
  checks, frontend tests/build и обновлённые security/performance gates.

## Не входит

- перенос модулей в микросервисы;
- замена PostgreSQL/Kafka/Valkey или обход существующих abstractions;
- provider-specific branching в Core;
- ослабление uniform pre-verification webhook response, RLS, approval,
  audit/redaction или SecretProvider boundaries;
- объявление production capacity без измерений на целевой topology.
