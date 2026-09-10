# Task 234 — Security correctness и performance hardening

## Статус

`in_progress` — follow-up по результатам security/performance-аудитов;
A01–A10 из аудита 2026-09-08 исправлены в указанной ниже области.

```yaml
repository_status: in_progress
release_blockers: [234.1, 234.3]
security_priority: high
external_evidence_required: false
```

## Выполнено 2026-09-10 — A10 / таймауты и восстановление SSE

- [x] После auth/tenant/authz SSE использует отдельный deadline на каждую
  запись/flush; между кадрами deadline снят. Общие таймауты API сохранены.
- [x] Ошибки write, Flush и управления deadline завершают поток; медленный
  клиент не удерживает обработчик бесконечно. Cancellation освобождает поток.
- [x] После каждого ready отправляется invalidate/reason connected, чтобы
  перечитать изменения за время разрыва; heartbeat остаётся только liveness.
- [x] Реальные HTTP/1.1 и HTTP/2: поток переживает общий timeout и два
  heartbeat-интервала, доставляет последующее изменение; проверены reconnect,
  401/403, обычный API timeout и backpressure через http.Server/net.Pipe.
- [x] Chrome с настоящими React/Query/SDK: пропущенное изменение видно после
  reconnect, восемь invalidate дают один refetch, logout отменяет SSE/retry.
- [x] Целевые Go/race, общий Go test/vet, contracts, architecture,
  frontend tests/build и generated SDK checks — PASS.
- [Отчёт и журналы](../../docs/audits/2026-09-10-a10-fix.md),
  [ADR-0189](../../adr/0189-sse-write-deadlines-and-reconnect-refresh.md).

A10 закрыта в репозитории; исходный список A01–A10 завершён. Для применения
обновить все API instances; миграция и изменение frontend runtime не нужны.
Рабочий Docker stack не пересобирался/перезапускался. Task 234 остаётся
`in_progress`: масштабирование SSE и остальные незавершённые подзадачи ниже
не входят в закрытые дефекты A01–A10.

## Выполнено 2026-09-10 — A09 / конкурентная регистрация OIDC-сессии

- [x] Первый INSERT идемпотентен; проигравший конкурент читает созданную
  строку заново под блокировкой и проверяет status/subject. Только создатель
  записывает `session_observed`, атомарно с сессией.
- [x] На реальной PostgreSQL с forced RLS и двумя application pools:
  8 одновременных первых API-запросов успешны, одна сессия и одно событие.
  До исправления тот же тест давал 7 ложных 401 из 8 запросов.
- [x] Проверены оба порядка Observe/Revoke, отказ после отзыва, cancellation,
  rollback при ошибке login event, retry, неизменность subject и tenant scope.
- [x] Сбой хранилища сессий возвращает 503 без раскрытия DB-ошибки и без
  запуска бизнес-обработчика; неверная/отозванная сессия по-прежнему даёт 401.
  Поведение описано в OpenAPI, generated SDK hashes обновлены.
- [x] Целевые unit/contract и PostgreSQL `-count=1 -race`, общий Go test,
  vet, contracts, architecture, SDK и TypeScript declarations — PASS.
- [Отчёт и журналы](../../docs/audits/2026-09-10-a09-fix.md),
  [ADR-0188](../../adr/0188-concurrent-oidc-session-observation.md).

A09 закрыта в репозитории. Для применения обновить все API instances;
миграция и переписывание истории сессий не нужны. Развёртывание не выполнялось.
После следующего продолжения A10 также закрыта. Общая Task 234 сохраняет `in_progress`:
JWKS, уменьшение количества membership/IdP calls, throttle last_seen и метрики
из 234.6, а также остальные незавершённые подзадачи, остаются открытыми.

## Выполнено 2026-09-09 — A02 / подтверждение email приглашения

- [x] Привязка приглашения требует email и boolean `email_verified=true`
  из одного аутентифицированного UserInfo response с совпадающим subject.
- [x] Profile email отделён от invitation proof; token/profile fallback и
  подтверждение другого адреса не разрешают привязку. `false`, `null` и
  отсутствие признака означают отсутствие доказательства владения.
- [x] Реальная PostgreSQL + полная auth/tenant/authz цепочка: попытка получить
  приглашённую admin-роль без proof отклоняется, приглашение не меняется.
  Подтверждённый владелец принимает его один раз; retry не меняет version.
- [x] Проверены bound viewer, disabled member, другой workspace, неверный
  subject/тип claim, неизменность публичных отказов и отсутствие нового PII
  в public projection. Вход существующего активного участника сохранён.
- [x] Целевые unit/contract и PostgreSQL тесты с `-count=1 -race`, общий
  `go test ./...`, vet, contracts, architecture и SDK checks — PASS.
- [Отчёт и журналы](../../docs/audits/2026-09-09-a02-fix.md),
  [ADR-0187](../../adr/0187-verified-email-invitation-binding.md).

A02 закрыта в репозитории; развёртывание API не выполнялось. Для применения
обновить все API instances и проверить UserInfo mapper провайдера. Старые
привязки автоматически не отзываются: их проверка описана в ADR-0187.
A09 и A10 закрыты в продолжениях от 2026-09-10; оставшиеся подзадачи ниже открыты.

## Выполнено 2026-09-09 — A01 / изоляция браузерного кеша

- [x] Отдельный QueryClient и дерево UI для каждого контекста
  пользователя/workspace/прав; обычное продление scoped-сессии сохраняет кеш.
- [x] Logout немедленно отменяет запросы и очищает старое состояние, включая
  случай медленного logout в host adapter. Истечение и ошибки сессии работают
  fail-closed; старый таймер не завершает уже продлённую сессию.
- [x] Поздние API/OIDC ответы не восстанавливают A, не меняют сессию B и не
  повторяют старую команду с правами нового пользователя.
- [x] Браузерный сценарий A → logout → B: загрузка, ошибка API B и успешный
  повтор; поздние 200/401 A, смена workspace/прав, истечение сессии.
- [x] 22 целевых logic-теста, общий frontend gate с production build,
  `go test ./...`, `go vet ./...`, contracts и architecture — PASS.
- [Отчёт и ограничения проверки](../../docs/audits/2026-09-09-a01-fix.md),
  [ADR-0186](../../adr/0186-frontend-session-cache-lifetime.md).

A01 закрыта в репозитории. A09 и A10 закрыты 2026-09-10; незавершённые
подзадачи ниже открыты. Развёртывание frontend не выполнялось; после обновления артефакта
существующие вкладки нужно перезагрузить.

## Выполнено 2026-09-09 — webhook A03–A04 / 234.2

- `ApplyVerifiedWebhook` атомарно фиксирует receipt, payment transition, audit
  и outbox. Ошибка, конфликт версии и отказ COMMIT откатывают receipt.
- Проверенные payment/commerce/social deliveries получают 503 при ошибке
  сохранения; pre-verification ответы остаются uniform 200.
- Исправлены повтор commerce-события без provider timestamp и сохранение
  типизированной ссылки на webhook secret в runtime config.
- Реальная PostgreSQL: все стадии failure injection, конфликт версии,
  concurrent replay, потеря DB connection, проверка неизменности payload.
  Реальные WooCommerce/Telegram/MAX verifiers: неверная подпись/секрет,
  outbox failure, redelivery; обычный Inbox fingerprint остаётся строгим.
- [Отчёт](../../docs/audits/2026-09-09-webhook-fixes.md),
  [ADR-0185](../../adr/0185-verified-webhook-atomic-commit-and-retry.md).

## Выполнено 2026-09-09 — A05–A08

- A05: pending intent после неопределённого create; retry без повторного remote
  вызова; YooKassa `metadata.external_id` → SDK observation → проверка
  account/amount/currency → восстановление remote binding. Исправлен формат
  payment reconciliation audit ID на UUIDv7.
- A06 / часть 234.3: общая транзакция mutation + audit для member, workspace,
  profile/avatar removal, identity provider и connector runtime config.
  Остальной inventory security/connector-account writes остаётся открытым.
- A07 / 234.4: случайные subscription references с SHA-256 в tenant/account
  runtime config, exact topic, проверка до Inbox, revocation, OpenAPI/SDK и
  инструкция maintenance window.
- A08 / основная часть 234.5: единое соединение для lock/read/rotation; реальная
  PostgreSQL с pool=1/4, cancel/rejected refresh и rollback. Пул не увеличивался;
  дополнительные метрики и jitter из 234.5 остаются follow-up.
- Воспроизведение: `./scripts/check-audit-postgres.sh` (полный migration catalog,
  непривилегированный application role, локальный HTTP mock, `go test -race`).
- [Подробности и границы результата](../../docs/audits/2026-09-09-a05-a08-fixes.md),
  [ADR-0184](../../adr/0184-audit-transaction-and-webhook-topic-binding.md).

Дополнительный follow-up: согласовать family storefront между каталогом,
Go-манифестами WooCommerce/Saleor и PostgreSQL с миграцией существующих аккаунтов;
расширить recovery beyond bounded provider list/window для pending платежей.

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

- [x] Перед count/update сериализовать изменения admin-состава на уровне
  `(organization_id, workspace_id)`: tenant-scoped advisory transaction lock,
  блокировка полного набора активных admin в детерминированном порядке либо
  `SERIALIZABLE` с ограниченным retry.
- [x] Не считать блокировку только изменяемой строки достаточной защитой.
- [x] Сохранить optimistic `expected_version` и default-deny authorization.
- [x] Привязать `Idempotency-Key` к digest нормализованного payload; повтор с
  тем же ключом и другим role/status должен завершаться конфликтом.
- [x] Добавить PostgreSQL-тест: два параллельных запроса отключают двух разных
  admin; ровно один коммитится, после завершения остаётся один active admin.
- [x] Добавить тесты concurrent demotion, disable и корректного replay одного
  и того же payload.

### 234.2 — Сделать verified payment webhook атомарным и повторяемым

- [x] После удалённой проверки передавать нормализованный verified result в
  один repository boundary, который атомарно фиксирует receipt, меняет payment,
  добавляет audit и Transactional Outbox event.
- [x] Не переводить receipt в terminal/applied до успешного status transition.
  Если выбран `pending/applied/rejected` state machine, pending должен иметь
  lease/retry/DLQ и наблюдаемую причину остановки.
- [x] После доказанной подлинности внутренняя ошибка должна либо дать провайдеру
  retryable ответ, либо быть надёжно поставлена в durable retry до `2xx`.
  До verification сохранить одинаковый ответ без account enumeration.
- [x] Повтор уже применённой доставки остаётся no-op и не создаёт повторных
  audit/outbox side effects.
- [x] Добавить failure-injection тесты: receipt insert success + transition
  failure, optimistic conflict, DB outage и redelivery; существующие PostgreSQL-сценарии worker reconciliation
  также проходят в общем gate A03–A08.
- [x] Подтвердить, что reconciliation остаётся safety net, а не единственным
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
- [x] Добавить тесты с injected audit failure для role/status, identity-provider
  enable/disable и profile update.

### 234.4 — Привязать commerce webhook topic к доверенному ожиданию

- [x] Перестать формировать `ExpectedTopic` из того же HTTP-заголовка, который
  заполняет `HeaderTopic`.
- [x] Для провайдеров, у которых topic не входит в подпись body, выдавать
  отдельный непредсказуемый subscription endpoint/reference, серверно
  связанный с account и exact expected topic.
- [x] Сравнивать provider header с серверной subscription configuration до
  dedup/outbox claim; для подписанного event в body дополнительно проверять
  семантическое совпадение внутри connector adapter.
- [x] Delivery fingerprint не должен позволять replay одного подписанного body
  сначала зарегистрировать под ложным topic и поглотить корректную доставку.
- [x] Обновить OpenAPI, migration/compatibility notes и connector conformance
  fixtures для WooCommerce и Saleor.

### 234.5 — Убрать nested-transaction deadlock из OAuth refresh

- [x] Advisory lock, повторное чтение bundle и encrypted rotation должны
  использовать один явно переданный SQL transaction/connection boundary либо
  отдельный coordinator pool с гарантированным резервом connections.
- [x] Не допускать схему, где все connections удерживают outer lock-транзакции
  и одновременно ждут nested `SecretProvider.Use/Rotate`.
- [x] Сохранить distributed serialization между API/worker и не выполнять два
  remote refresh для rotating refresh token.
- [ ] Добавить bounded concurrency, jittered backoff и метрики lock wait,
  refresh latency/failure и pool saturation.
- [x] Добавить тесты для `MaxOpenConns=1`, pool-size concurrent accounts,
  timeout/cancel, rejected refresh и rotated-token replay.
- [ ] Если минимальный размер пула временно повышается, валидировать его при
  startup и задокументировать как mitigation, а не окончательное исправление.

### 234.6 — Сократить OIDC/session/membership hot path

- [x] A09: идемпотентная первая регистрация сессии, единственное login event,
  сериализация с Revoke и различение 401/503; PostgreSQL regression с двумя
  пулами и восемью одновременными запросами. См. ADR-0188.
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

- [x] Для SSE явно снять или продлевать write deadline после прохождения
  обычной auth/tenant/authz композиции; общие HTTP timeouts для остальных
  маршрутов не ослаблять.
- [x] Добавить integration-тест с реальным `http.Server`: stream переживает
  configured `WriteTimeout`, получает heartbeat и завершается по cancel.
- [x] A10: bounded frame write/Flush и refresh при reconnect, включая
  пропущенные изменения, прежний/пустой cursor; браузерная проверка coalescing.
- [ ] Определить bounded срок жизни/повторную проверку полномочий открытого
  SSE при expiry токена, отзыве сессии и изменении прав; сейчас authn/authz
  выполняются при установлении соединения, а не для каждого кадра.
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
