# Task 231 — Экосистема и поддержка: integrations, apps, partners, mobile и cloud SLA

## Статус

`repository-complete` — ecosystem/support control plane реализован поверх
Connector SDK, conformance/readiness, plugin marketplace, customer service,
mobile WMS, Cloud billing, SLO/DR и Community deployment foundations.

В репозитории закрыты контракты, typed core, tenant-scoped onboarding и
partner evidence, API/OpenAPI/SDK/MCP, `/ecosystem` frontend, RLS migration,
audit/idempotency guards и synthetic qualification. Статусы в сводке строятся
по evidence и не подменяют внешнюю квалификацию.

Показатели ChannelEngine/Linnworks используются только как ориентир масштаба.
Искусственное увеличение числа манифестов без runtime value не считается
результатом.

### Что закрыто в репозитории

- 231.1–231.2: provider-neutral portfolio tiers, owner/priority/decision/
  next-action и честные `integrated`–`supported` status gates поверх readiness
  matrix;
- 231.3–231.4: повторяемый onboarding с bounded checks, fail-closed states,
  idempotency, capability/evidence projection и explicit qualification boundary;
- 231.5–231.6: customer-facing app/developer projection, generated API/SDK,
  MCP read surface и reuse governed plugin marketplace/n8n contracts;
- 231.7: partner tiers и append-only certification evidence с expiry/revocation;
- 231.8–231.11: mobile, hosted, support и billing/packaging projections
  подключены к существующим bounded contexts без второго ledger/backend;
- 231.12: metrics, demo-safe projection, frontend workspace, events, static
  qualification и external release-gate register.

Поддержка и production SLA не объявлены автоматически: для них остаются
credentialed, partner, topology/DR, device и operational evidence gates.

## Цель

Построить повторяемую систему роста и поддержки:

```text
официальный connector → conformance → ready capability → app listing
→ partner delivery → mobile/hosted operation → SLA/support evidence
```

Клиент должен найти нужную интеграцию или приложение, увидеть реальные
capabilities и ограничения, подключить его безопасно, получить внедрение от
обученного партнёра, работать из мобильного интерфейса и понимать, какой
уровень доступности и поддержки действительно гарантируется.

## Уровни готовности

- `integrated` — манифест и SDK-контракт существуют;
- `verified` — пройдены conformance, security и supply-chain проверки;
- `ready` — конкретная capability работает в runtime с evidence;
- `qualified` — выбранный сценарий прошёл sandbox/live qualification;
- `supported` — есть документация, owner, version policy и SLA/response target;
- `deprecated`/`blocked` — использование запрещено с понятной причиной.

Health-check, manifest, SDK, marketplace listing или маркетинговое описание
не поднимают элемент на следующий уровень автоматически.

## Подзадачи

### 231.1 — Стратегия портфеля и Definition of Done

**Зависимости:** 064, 078, 226.

- Разделить portfolio tiers: commerce, logistics, finance, ERP/CRM,
  notifications, identity, AI/social и specialized surfaces.
- Зафиксировать критерии `integrated`, `verified`, `ready`, `qualified`,
  `supported`, `deprecated` и `blocked` для connector, capability, app и
  partner service.
- Выбрать целевые customer journeys: catalog/price/order/fulfillment,
  finance, customer service и reporting.
- Для каждого tier определить owner, evidence, support level, compatibility,
  deprecation window и next action.
- Разделить Community self-hosted, hosted production и partner-managed
  responsibilities.

**Acceptance:** стратегия содержит measurable baseline, target waves и
ответственность; «готово» определяется evidence, а не количеством записей.

### 231.2 — Аудит и приоритизация connector portfolio

**Зависимости:** 226, 231.1.

- Использовать реестр Task 226 по всем коннекторам: capability, stage,
  health-only, runtime adapter, owner, official API, scopes и evidence age.
- Рассчитать priority по customer demand, business coverage, API quality,
  implementation effort, supportability и legal risk.
- Выбрать qualification wave 1/2/3 и список connector-ов, которые нужно
  углубить, оставить specialized/health-only или deprecate.
- Публиковать coverage по use case, стране, deployment и capability, а не
  только число connector ID.

**Acceptance:** каждый connector имеет tier, owner, priority, status, evidence
age и next action; roadmap показывает покрытые и незакрытые сценарии.

### 231.3 — Connector factory и повторяемый onboarding

**Зависимости:** 010, 064, 226, 231.2.

- Сделать шаблон: manifest, typed SDK, capability audit, auth/SecretProvider,
  fixtures, conformance, docs и runtime registration.
- Включить CI checks для contracts, architecture, supply chain, security,
  scopes, timeout/rate limit, idempotency, webhooks и read-after-write.
- Подготовить sandbox emulator/fixture kit и redacted evidence bundle.
- Автоматизировать reviewer sign-off, versioned release, rollback и
  deprecation checklist.
- Запретить прямой доступ connector-а к Core/DB или обход host policy.

**Acceptance:** новый verified connector проходит воспроизводимый PR →
conformance → runtime pipeline; пропущенный required check блокирует admission.

### 231.4 — Углубление приоритетных интеграций

**Зависимости:** 231.2–231.3, 217, 220, 221, 222, 224, 225, 226.

- Для первой волны довести выбранные connector-ы до vertical slice:
  account → sync/read → normalized model → operator action/write → webhook/
  status → reconciliation.
- Квалифицировать capability по доменам: catalog, prices, inventory, orders,
  fulfillment, returns, settlement, ads и customer service.
- Для writes подтвердить approval/risk, idempotency, unknown outcome,
  read-after-write, rate limits, retries и manual recovery.
- Показывать `read_only`, `partial`, `ready`, `qualification_required` в API,
  UI, SDK, worker и MCP одинаково.

**Acceptance:** каждая заявленная capability имеет актуальное runtime/live или
sandbox evidence; health-only не показывается готовым бизнес-решением.

### 231.5 — Marketplace apps и distribution

**Зависимости:** 078, 231.1–231.4.

- Создать каталог apps/connectors/solutions с поиском, фильтрами по domain,
  стране, use case, deployment, pricing, status и required scopes.
- В listing показывать publisher, version, artifact digest, trust, license,
  security contact, capabilities, data classes, network destinations,
  limitations, compatibility и support owner.
- Добавить install/update/rollback/revoke с exact artifact consent, approval,
  dependency graph, release notes и migration impact.
- Разделить first-party, verified partner, community/private и health-only;
  private listings не должны пересекать tenant.
- Отзывы и рейтинги считать untrusted feedback и не использовать для security
  admission.

**Acceptance:** до установки клиент видит permissions/secret/network/data
impact; изменение digest требует re-consent; revoked app не запускается.

### 231.6 — Developer platform и partner API

**Зависимости:** 010, 019, 062, 064, 231.3.

- Опубликовать versioned SDK/API, OpenAPI, event schemas, capability catalog,
  examples, sandbox, test tenants и conformance runner.
- Создать partner portal: application, credentials/scopes, webhooks, rate-limit
  dashboard, evidence upload, support contact и changelog.
- Довести внешний `n8n-nodes-torgnexa` package до поддерживаемого distribution
  slice: versioned artifact, credential setup, Product/Order nodes, canonical
  event trigger, webhook subscribe/disable, signed delivery, retry/error
  guidance и compatibility matrix.
- Не добавлять n8n privileged path: node использует только public `/api/v1`,
  tenant/workspace берётся из authenticated identity, а sensitive writes
  остаются approval/policy/idempotency-gated.
- Зафиксировать semantic versioning, compatibility/deprecation policy,
  migration guides и security disclosure.
- Разделить developer, implementation partner и publisher roles с least
  privilege access.
- Запретить партнёру raw tenant secrets, private keys, arbitrary DB/network
  access и policy bypass.

**Acceptance:** внешний разработчик может собрать typed connector и пройти
sandbox conformance без доступа к repository internals или production data;
n8n package устанавливается из versioned artifact, проходит проверки подписи
webhook, replay/redirect/error handling и не может обойти host permissions.

### 231.7 — Партнёрское внедрение и certification

**Зависимости:** 231.1–231.6.

- Ввести tiers: referral, implementation, certified solution, managed
  operations и support escalation.
- Подготовить curriculum и экзамен по tenant setup, mapping, PIM, orders/WMS,
  finance, approvals, security, backup/restore и incident response.
- Создать playbooks discovery → design → migration → sandbox UAT → cutover →
  rollback → training → hypercare → handoff.
- Дать партнёру sandbox/demo workspace с masked evidence и scoped credentials.
- Вести срок certification, customer feedback, quality score, security
  incident, conflict of interest и revocation.

**Acceptance:** certified partner по checklist проводит sandbox-to-production
readiness; production claim невозможен без UAT и rollback evidence.

### 231.8 — Mobile product и delivery

**Зависимости:** 229, 231.1.

- Определить mobile boundaries: warehouse pick/pack/scan/print, approvals,
  incidents, customer service и read-only management views.
- Выбрать первый target (responsive PWA/handheld app), supported OS/devices,
  release channels, scanner/camera/printer и offline policy.
- Использовать существующие API/SDK и единую capability/permission/policy
  matrix; mobile не становится вторым backend.
- Поддержать device enrollment/revoke, crash reporting без PII, remote config,
  compatibility, update/rollback и accessibility.
- Offline queue должна иметь expiration, conflict/replay и server receipt;
  external writes не считаются выполненными локально.

**Acceptance:** mobile vertical slice проходит device matrix, authenticated
E2E, offline/reconnect, scanner/printer, permission, crash/recovery и release
checks.

### 231.9 — Cloud offering, SLO/SLA и multi-tenant operations

**Зависимости:** 027, 093, 118, 226, 231.1.

- Определить hosted tiers, regions/data residency, environments, backup/
  restore, RPO/RTO, maintenance, quotas, scaling и fair-use limits.
- Зафиксировать SLI/SLO и customer SLA для API, sync freshness, webhook
  delivery, worker recovery, report generation, support response и durability.
- Создать status page, incident communication, maintenance/degradation policy,
  service-credit/claim process и evidence retention.
- Провести tenant isolation, encryption, key rotation, egress, audit export и
  disaster-recovery drills.
- Не обещать hosted SLA для Community/self-hosted или third-party operation,
  которая остаётся `read_only`/`unknown`.

**Acceptance:** SLA terms связаны с измеримыми metrics/topology; DR/restore и
outage drills оставляют evidence; status page не раскрывает PII/secrets.

### 231.10 — Support desk, onboarding и customer success

**Зависимости:** 231.5, 231.7, 231.9.

- Связать support intake с unified inbox/cases: tenant, connector,
  capability, run, correlation ID, impact, priority и SLA.
- Разделить L1/L2/L3/partner/provider escalation, ownership, duty rota,
  business hours и response/resolution targets.
- Добавить onboarding checklist, readiness report, guided setup, demo data,
  diagnostics, safe redacted log bundle и knowledge base.
- Поддержать customer-visible incident timeline, RCA, workaround,
  retry/reconcile instructions и closure survey.
- Не просить в support-чате токены, полные bank/payment data или production PII.

**Acceptance:** support case связывается с connector/run без утечки PII, SLA
эскалирует корректно, diagnostic bundle redacted, лишних write permissions нет.

### 231.11 — Billing, packaging и governance

**Зависимости:** 231.5, 231.7, 231.9.

- Определить packaging по hosted tier, operations/connectors, seats, storage,
  mobile devices, support и partner service.
- Разделить cloud subscription billing, marketplace payments и seller
  settlement/P&L; не смешивать их в одном ledger.
- Добавить usage metering, trial/sandbox limits, invoice evidence,
  suspension/grace period и append-only correction.
- Проверить publisher identity, signed artifact, SBOM/provenance, malware,
  license, security contact, data processing и vulnerability disclosure.
- Exact consent для artifact/version/capability/secret/network/resource
  changes; revocation и emergency kill switch обязательны.

**Acceptance:** usage deterministic и tenant-scoped; revoked app/partner
теряет доступ; plan/SLA terms видны до активации; governance evidence сохранён.

### 231.12 — Ecosystem metrics, demo и release gate

**Зависимости:** все предыдущие подзадачи.

- Разделять counts `integrated/verified/ready/qualified/supported` и outcome
  metrics: successful operations, read-after-write, reconciliation, support
  resolution, mobile adoption, SLO и incident recovery.
- Добавить synthetic app/connector listings, partner tenant, mobile device,
  hosted tier, support case, quota, incident, revocation и SLA breach.
- Пройти authenticated E2E: developer onboarding → conformance → listing →
  install/consent → partner sandbox → mobile use → support escalation →
  incident/status → resolution/rollback.
- Отдельно проверить n8n: credential setup → Product/Order read → canonical
  event trigger → signed webhook delivery → duplicate/replay rejection →
  disable/revoke → version update с compatibility check.
- Проверить wrong scope, health-only mislabel, private listing isolation,
  partner access, cloud outage/DR, mobile offline, quota и billing correction.
- Release claim разделить на repository readiness, Community, hosted SLA,
  partner certification и live connector qualification.

**Acceptance:** customer path повторяем; unsupported/revoked assets не
запускаются, SLA работает, mobile/hosted evidence сохранены, маркетинговый
count не заменяет operational proof.

## Архитектурные ограничения

- Ecosystem работает через Connector SDK, capabilities, host policy,
  SecretProvider, Outbox/Inbox и существующие bounded contexts.
- Нельзя давать app/partner прямой доступ к DB, tenant secrets, private keys
  или произвольной сети.
- Readiness, qualification и SLA scoped к capability, account, topology,
  version и evidence age; manifest не равен production support.
- Customer/partner data, support access, logs и diagnostics минимизируются,
  tenant-scoped и redacted; production PII запрещён в fixtures.
- Community/self-hosted, hosted и partner-managed operation не выдают друг
  другу SLA или security guarantees.

## Не входит в этот task

- Искусственное добавление сотен манифестов без runtime value.
- Покупка чужого integration directory или undocumented API.
- Собственная ERP/CRM/helpdesk/billing platform вместо интеграции с Core.
- Обещание 99.9%+ cloud SLA до topology/DR/observability qualification.

## Зависимости

010, 019, 025, 027, 062, 064, 078, 093, 118, 226, 229.

## Definition of Done

- Все 12 подзадач имеют repository implementation или явный reuse канонического
  bounded context; новые контракты, docs, frontend, SDK/MCP и security gates
  присутствуют.
- Есть customer-facing ecosystem catalog, developer/partner onboarding,
  certification evidence, mobile/hosted/support projections и observed metrics.
- Support desk, case escalation, status page, usage, audit, quotas и revoke
  controls остаются владельцами существующих модулей и отображаются без
  дублирования ledger/CRM/backend.
- Для каждого `ready/qualified/supported` есть актуальное evidence; остальные
  статусы честно отображаются в UI/API/SDK.
- Пройдены или запускаются repository gates: `gofmt`, targeted/full Go tests,
  `go vet ./...`, `./scripts/check-contracts.sh`, `make migrations`,
  `make ecosystem-support-qualification`, SDK/frontend checks.
- Внешние gates отдельно перечислены и не маскируются под repository PASS:
  credentialed connector writes/read-after-write, partner UAT/rollback, hosted
  topology SLO/DR, mobile device matrix и production support/SLA.
