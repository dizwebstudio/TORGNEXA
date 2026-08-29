# Task 166 — Центр качества публикации товаров

## Status

`planned` — подробная декомпозиция подготовлена, реализация не начата.

## Objective

Создать единый «Центр качества публикации товаров», который до remote write
показывает готовность Product/Offer к публикации на конкретный connector
account, объясняет каждую проблему и не допускает публикацию при hard-blocker.
Центр должен проверять канонические данные каталога/PIM, цены и остатки,
медиа, обязательные атрибуты и категории, compliance evidence, mapping,
capability и freshness источников.

Результат должен быть различим по target: один товар может быть готов для
одного канала и заблокирован для другого. Нужны карточка товара, score с
прозрачной формулой, список ошибок/warnings, ссылки на исправление, массовая
проверка, история запусков и post-publish drift. «Готов» означает совпадение
проверенного snapshot с отправляемой версией и наличие актуальной capability/
compliance evidence, а не просто заполненный Product.

Сейчас в репозитории есть базовые Product/Offer/PIM-модели, отдельный
compliance evaluator и fail-closed guard для `products.write`, но нет
агрегированного quality run/profile, target-level score, UI-центра или durable
preflight перед `commerce-sync`. Сам product writer передаёт ограниченный
набор `SKU/title/description/status`, а обязательные channel attributes,
media constraints и mapping validity проверяются не единым доменным контуром.
Task 166 закрывает этот разрыв, не добавляя provider-specific поля в Product и
не заменяя Task 082 compliance guard.

## Architecture boundaries

- Product/Offer, PIM attributes, Price, InventoryPosition, media release и
  ComplianceDocument остаются источниками фактов в своих доменах. Quality
  Center хранит derived evaluation/snapshot/evidence, но не редактирует
  канонические значения и не становится вторым PIM.
- PostgreSQL — источник истины для quality profiles, rules, runs, issues,
  decisions, target snapshots и gate receipts. ClickHouse может хранить
  агрегаты/тренды качества, но не разрешает publication write.
- Общие правила живут в provider-neutral engine. Connector даёт
  versioned declarative publication profile (required fields, limits, category/
  attribute mapping, media/price constraints) через SDK/runtime capability;
  Core не ветвится по `ozon`, `wildberries` или любому другому provider id.
- Task 082 `compliance.Evaluator` и host `complianceguard` остаются финальным
  fail-closed барьером перед `products.write`. Quality block не может быть
  обойдён кнопкой UI, MCP, n8n или workflow; допустимый override определяется
  policy и Task-017 approval с audit/evidence.
- `commerce-sync` и другие publication routes используют quality receipt,
  связанный с exact local version, target account, profile/ruleset digest,
  capability snapshot и compliance fingerprint. Нет актуального PASS — нет
  remote call; remote rejection/unknown outcome идёт в reconciliation, не в
  ложный success.
- События публикуются через Transactional Outbox и обрабатываются Inbox/
  deduplication. Сырые provider payloads, credentials, Authorization headers,
  несанитизированный HTML/JS и лишний PII в quality evidence не сохраняются.
- Все списки и batch runs ограничены по tenant/workspace, target, product count,
  media size, rule depth и времени. Valkey может кэшировать profile/read model,
  но cache miss не меняет decision и cache не является truth.

## Quality decision vocabulary

Точные enum и совместимость закрепляются в 166.1–166.2, но implementation
обязана различать:

- `ready` — hard checks pass, snapshot/profile/compliance/capability свежие;
- `ready_with_warnings` — publication разрешена, но есть не блокирующие
  рекомендации;
- `blocked` — обязательное поле/asset/mapping/compliance/capability недоступно
  или нарушено ограничение;
- `approval_required` — policy разрешает продолжение только через Task-017;
- `stale` — проверка относится к другой версии или истёк TTL evidence;
- `unsupported`/`not_configured` — target или capability не admitted/configured;
- `unknown` — evaluation не завершилась или remote preflight дал
  неоднозначный результат; автоматическая публикация запрещена.

Score (например, 0–100) является объяснимой производной метрикой и никогда не
перевешивает `blocked`, `approval_required`, `stale` или `unknown`.

## Subtasks and implementation order

### 166.1 — ADR, scope, terminology and rule governance

**Depends on:** none.

- Зафиксировать ADR для Product Publication Quality Center и его границу с
  Product/PIM, Compliance, Sync/Reconciliation, Connector SDK и Social Core.
- Утвердить vocabulary: target, publication profile, rule, check run, issue,
  severity, score, snapshot, freshness, gate receipt, override, remediation,
  remote preflight и post-publish drift.
- Определить обязательные quality categories: identity/content, category/
  attributes, media, price/stock, compliance, mapping/capability, localization
  и channel contract.
- Утвердить severity/decision policy: `block`, `approval_required`, `warn`,
  `info`; hard blockers не overrideable, soft blockers — только через reviewed
  policy/approval; score weights и rounding versioned.
- Определить target scope: Product vs Offer, connector account, market/channel,
  jurisdiction, locale и publication mode; зафиксировать TTL и invalidation
  причины.
- Согласовать совместимость с Tasks 004/023/082/119/161 и release gate;
  quality center не должен обещать capability, которой нет в runtime support.

**Acceptance:** ADR и governance matrix одобрены; decision vocabulary,
hard/soft blockers, score formula, override rules и ownership каждой проверки
однозначны; никакой provider branch не попадает в Core.

### 166.2 — Canonical quality model and immutable evaluation snapshot

**Depends on:** 166.1.

- Ввести provider-neutral типы `QualityProfileRef`, `QualityRule`,
  `QualityCheckRun`, `QualityIssue`, `QualityScore`, `QualityDecision`,
  `PublicationGateReceipt` и `RemediationAction`.
- Snapshot должен включать product/offer/PIM versions, price/inventory/media
  release refs, target account, connector capability snapshot, profile/rule
  digest, compliance fingerprint, locale/jurisdiction и evaluated_at/valid_until.
- Issue хранит только code, severity, field/path, bounded message, expected vs
  observed metadata, remediation hint и source reference; secrets/raw payloads
  запрещены.
- Зафиксировать lifecycle run: `queued -> running -> completed | failed |
  cancelled`, decision terminality и правила supersede при новой версии товара.
- Receipt должен быть exact-match по target + local versions + digests; изменение
  любого входа делает старое `ready` непригодным для remote write.
- Предусмотреть aggregate score и category scores с детерминированной сортировкой
  issues, чтобы повторный run с теми же inputs дал тот же результат.

**Acceptance:** domain tests покрывают target-specific decision, snapshot
collision, stale receipt, deterministic score/order, immutable completed run,
unknown/approval/block outcomes и cross-tenant ID rejection.

### 166.3 — Versioned publication profiles and declarative rule schema

**Depends on:** 166.1, 166.2.

- Добавить Draft 2020-12 schema для profile/rule contract: required canonical
  fields, typed attribute codes, enum/range/length constraints, locale,
  currency/unit, media count/dimensions/format, price/stock rules and status
  mapping references.
- Profile должен объявлять только проверяемые capability/profile facts, а не
  давать connector произвольный код, SQL, HTTP, regex с ReDoS или secret access.
- Сгенерировать validated profile metadata из connector manifest/runtime support;
  неизвестные fields, duplicate rule IDs, циклические references, oversized
  graph и unbounded expressions fail closed.
- Разделить global canonical rules, connector-family rules и account/category
  mapping config; provider-native names остаются в adapter/profile boundary.
- Версионировать rule/profile, дать migration/compatibility policy и сохранить
  старые profiles для исторических runs; новая версия не меняет прошлое решение.
- Разрешить tenant custom rules только в ограниченном typed allowlist с лимитами
  и review/approval; произвольный JavaScript/Python/LLM expression запрещён.

**Acceptance:** schema fixtures проверяют valid profile, unknown field,
type mismatch, unsafe/oversized expression, duplicate/recursive rules,
incompatible profile update и deterministic profile digest; generated catalog
содержит только admitted targets.

### 166.4 — Snapshot assembly: catalog, PIM, prices, stock, media and compliance

**Depends on:** 166.2, 166.3.

- Собрать bounded read model из Product/Offer, Brand/Category/AttributeValue,
  Price, InventoryPosition/WMS, released catalog images/uploads, mappings,
  account settings и compliance evaluation.
- Проверять identity/GTIN/SKU, lifecycle, duplicate offers, category/brand,
  required attributes, localization/text, price/currency, stock policy,
  dimensions/weight и media metadata без бинарного или raw payload копирования.
- Медиа учитывать только после upload-security/ClamAV pipeline release;
  quarantined, missing, broken, oversized или unsafe assets дают issue и не
  запускают remote fetch. Arbitrary image URL fetch должен проходить
  существующий SSRF/egress boundary.
- Вызвать Task-082 evaluator с target channel family, jurisdiction, category,
  Offer/SKU/GTIN и seller context; не дублировать compliance decision в новом
  engine.
- Фиксировать source versions/freshness и missing-input reason; `unknown` не
  превращать в pass/zero/empty string.
- Не делать N+1: batch-load references, bounded cursor pages и deterministic
  ordering для большого каталога.

**Acceptance:** synthetic snapshots на valid/invalid GTIN, missing attributes,
expired compliance, price currency mismatch, unavailable stock, quarantined
media, archived product, duplicate SKU, stale mapping и cross-tenant data дают
ожидаемый input-quality result без сетевого вызова provider.

### 166.5 — Deterministic quality engine, score and remediation hints

**Depends on:** 166.3, 166.4.

- Реализовать rule evaluation над typed snapshot: required/presence,
  length/charset, enum/range, dependency, category mapping, media, price/stock,
  localization, compliance and capability checks.
- Разделить `block`, `approval_required`, `warn`, `info` и profile unavailable;
  hard blocker всегда доминирует над score.
- Рассчитывать category/overall score fixed-point/BPS, с versioned weights,
  deterministic rounding и объяснением вклада каждой категории.
- Issue code должен быть стабильным и локализуемым; raw provider validation
  response нормализуется в bounded reason code + safe field path.
- Выдавать remediation hint/route («заполнить атрибут», «загрузить asset»,
  «настроить mapping», «обновить документ») без автоматического изменения
  Product/PIM.
- Проверять duplicate/similar SKU и field conflicts через existing PIM
  evidence, но не выполнять destructive merge.

**Acceptance:** property/table tests подтверждают monotonic severity, stable
score, no false ready with hard blocker, localization-safe messages,
duplicate issue suppression и identical output for identical snapshot/profile.

### 166.6 — Pre-publication gate in sync/runtime and compliance guard

**Depends on:** 166.2, 166.5.

- Ввести host-side `QualityGate` port, который `commerce-sync` вызывает после
  policy/account/capability checks и до `ProductWriter`/remote egress.
- Gate требует receipt для exact Product/Offer versions, target account,
  profile/rule digest and compliance fingerprint; stale/missing/unknown result
  запускает bounded evaluation or defers event, но не делает remote call.
- Сохранить Task-082 `NewGuardedSession` как второй/final guard; quality pass не
  заменяет compliance authorization и approval.
- При `approval_required` создать Task-017 request с target, snapshot digest,
  issue list и risk; после approval re-read all inputs and recompute gate.
- При remote validation rejection записать `remote_contract_mismatch` quality
  issue and reconciliation evidence; не переводить товар в published/active
  только по HTTP 2xx.
- Для `products.write`, price/stock/status routes и future listing routes
  определить, какие quality categories mandatory; unsupported route remains
  explicitly `not_configured`/`unsupported`.

**Acceptance:** route tests prove blocked/stale/approval/ready behavior before
network, compliance denial still wins, approval cannot authorize changed
snapshot, duplicate event uses same receipt, remote rejection creates issue,
and no connector-specific branch appears in worker Core.

### 166.7 — Event triggers, durable scheduler and quality worker

**Depends on:** 166.4–166.6.

- Реагировать на canonical product/offer/PIM/price/inventory/media/compliance/
  mapping/policy changes через EventBus; target set определяется enabled
  tenant policies and current runtime support.
- Добавить scheduled full/bounded scans через Task-108 PostgreSQL-owned
  scheduler, с cursor/checkpoint и manual run; Kafka не используется как
  delayed-job store.
- Coalesce invalidations по `(tenant, product/offer, target)` и ограничить
  fan-out, чтобы массовый импорт не создавал quality storm на small VPS.
- Worker использует durable lease/fencing, Inbox/dedup, deterministic run ID и
  повторно проверяет tenant scope, profile version, account status, capability,
  compliance and source versions before commit.
- Retry only retry-safe DB/queue failures with jitter; malformed profile,
  missing mapping and quality block are terminal business outcomes.
- После run публиковать bounded `quality.*` events and invalidate publication
  gate only when exact snapshot receipt committed.

**Acceptance:** duplicate/out-of-order events, scheduler restart, lease loss,
worker crash at each persistence boundary, bounded catch-up and connector outage
do not duplicate runs or mark stale data ready; other tenants keep processing.

### 166.8 — PostgreSQL persistence, RLS, lineage and retention

**Depends on:** 166.2–166.7.

- Добавить expand-only migration для profile/rule versions, quality runs,
  snapshots/refs, issues, decisions, gate receipts, remediations и worker jobs;
  не ломать existing catalog/compliance/sync readers.
- Все tenant-owned tables — `FORCE ROW LEVEL SECURITY`, composite organization/
  workspace keys, optimistic versions и append-only guards для completed runs,
  issue history, approvals и evidence.
- Уникальности: one active run/target, deterministic `(snapshot, profile,
  ruleset)` digest, receipt idempotency, issue code/path, event/run dedup.
- Индексы для target/status/severity/updated_at, stale scans, blocked products,
  account/profile, cursor batches and operator filters; EXPLAIN bounded queries.
- Lineage links inputs and transformation/rule/model/profile versions to exact
  audit/event IDs; raw content remains in source domains or secure artifact
  storage and is not copied to quality rows.
- Retain immutable decisions/evidence according to audit/compliance policy;
  archive large completed check output without deleting legal/audit history.

**Acceptance:** migration static, fresh install/upgrade rehearsal, RLS negative
tests, append-only mutation denial, digest collision, retention/legal-hold and
bounded query-plan tests pass.

### 166.9 — REST/OpenAPI, permissions and operator UI

**Depends on:** 166.2, 166.5–166.8.

- Добавить tenant-scoped API для product/target quality summary, run/detail,
  issue list, batch scan, profile/rule preview, gate status, remediation,
  approval/override и post-publish drift; mutations require Idempotency-Key +
  optimistic version.
- Cursor pagination and server-side filters: connector/account, category,
  severity, status, freshness, score range, missing field and assignment.
- Обновить OpenAPI, generated SDK, permission matrix, MCP/n8n read/preview
  contracts и event catalog; tenant/workspace не принимаются из client payload.
- В UI добавить раздел «Качество публикации»: readiness counters, score cards,
  per-channel matrix, blocked/stale/approval queues, bulk scan, search and
  filters, progress, last run and freshness.
- Product drawer показывает category score, hard blockers first, exact field/
  attribute/media/compliance evidence, expected vs observed, fix link, target
  profile version, gate receipt and audit/lineage timeline.
- Bulk remediation только для typed safe fixes/commands; destructive or
  sensitive changes открывают обычный Product/PIM/approval flow. UI never hides
  a server denial or invents connector support.

**Acceptance:** operator can run one target check, understand every blocker,
open the correct fix flow, rerun idempotently, compare two target profiles and
see stale/unknown/approval states; API/UI expose no raw provider response or
secret.

### 166.10 — Remediation workflow, approvals and safe bulk actions

**Depends on:** 166.5, 166.6, 166.9.

- Classify remediation as read-only suggestion, reversible typed edit,
  approval-required change or manual/external action; store proposed diff and
  expected snapshot version before applying.
- Safe actions may update only existing Product/PIM/asset/mapping APIs with
  idempotency, optimistic version and audit/lineage; no direct SQL/HTTP or
  arbitrary generated content.
- Sensitive category/attribute/compliance/media/publication changes reuse
  Task-017 approval and Task-082 evaluator; after approval revalidate current
  profile, policy, source versions and target capability.
- Bulk jobs use bounded batches, per-tenant concurrency, partial progress,
  retry-safe commands and pause/cancel; one failed product does not poison the
  entire tenant batch.
- Provide preview/diff and dry-run before changes; preserve previous values and
  evidence for rollback through normal domain correction path.
- Optional AI suggestions remain non-authoritative, marked as suggestions and
  cannot auto-apply or bypass quality/compliance rules.

**Acceptance:** tests cover optimistic conflict, duplicate action, approval
expiry/rejection, stale proposed diff, partial batch failure, pause/resume,
cross-tenant denial and AI/MCP suggestion without mutation authority.

### 166.11 — Connector profiles, remote preflight and qualification

**Depends on:** 166.3, 166.6, 166.9.

- Для каждого admitted product/publication connector определить profile:
  required fields/attributes, category mapping, media constraints, price/stock
  semantics, status/lifecycle mapping, dry-run/preflight and read-after-write.
- Разделить SDK manifest, runtime route, quality profile and Docker/live
  qualification; manifest capability alone cannot make a product `ready`.
- Add deterministic fixtures for WooCommerce, OpenCart, PrestaShop, Bitrix24,
  Magento/Adobe Commerce, Medusa, Saleor, Shopify, Shopware and marketplace
  targets only where current runtime support exists; unsupported/health-only
  cards must return `unsupported` rather than generic PASS.
- If remote API offers validation/dry-run, call it through host runtime with
  timeout/rate limit and store normalized field errors only. If it does not,
  local profile remains advisory and remote rejection is reconciled evidence.
- Qualify mapping creation, category/attribute translation, locale/currency,
  media upload readiness, idempotency, rate limits, unknown response and
  read-after-write; never scrape browser/private endpoints.
- Generate capability/profile matrix and retain evidence bound to exact
  connector/profile/runtime versions.

**Acceptance:** generated profile/runtime matrix matches actual routes;
qualified target has conformance + Docker/live evidence, unqualified target is
fail-closed in API/UI/worker/automation, and remote errors never leak raw body.

### 166.12 — Security, observability, quotas and operations

**Depends on:** 166.7–166.11.

- Метрики: checks/min, run lag, freshness/stale ratio, ready/warn/block/
  approval/unknown counts, score distribution, issue top codes, gate denials,
  remote preflight latency/rejection, reconciliation lag, queue/DLQ and per-
  tenant saturation.
- Structured logs/traces contain only tenant/product/offer/target/run/profile/
  rule IDs, hashes, versions and bounded reason codes; redact PII, raw text,
  media URLs, secrets and provider payloads.
- Quotas: products/target per run, media/attribute size, rules/depth, events/min,
  concurrent runs, remote calls, batch actions, retention and API page limits.
  Breaches are deterministic and tenant-local.
- Threat model: cross-tenant target injection, profile tampering, stale receipt
  replay, rule bypass, malicious HTML/URL/regex, SSRF, zip/image bomb, PII leak,
  approval confusion and connector capability escalation.
- Prepare runbook for quality storm, stale profile, compliance expiry, mass
  blocked catalog, remote contract drift, duplicate publication, worker crash,
  stuck batch, false-positive rule and emergency publication pause.
- Add alert thresholds and operator actions without manual SQL mutation of
  canonical Product, Compliance, mapping or publication facts.

**Acceptance:** dashboards distinguish data-quality, policy, capability,
provider and infrastructure failures; kill switch/quota is tenant-local;
security tests prove profile/rule/evidence cannot grant publication authority.

### 166.13 — Tests, Compose E2E, screenshots and documentation

**Depends on:** all previous subtasks.

- Добавить Go unit/property/integration tests для snapshot assembly, typed rule
  engine, score, profiles, compliance integration, gate, repositories, RLS,
  Outbox/Inbox, worker, API, permissions and remediation.
- Compose E2E: synthetic Product/PIM/Offer/Price/Inventory/media/compliance →
  quality event → worker → target decision → `commerce-sync` gate → connector
  stub/store → read-after-write/reconciliation, включая blocked/stale/approval/
  unknown paths.
- Chaos/load cases: Kafka redelivery, PostgreSQL restart, worker crash before/
  after receipt, 10k+ products, profile version change, media quarantine,
  remote 429/5xx/validation rejection and simultaneous tenant scans; validate
  small-VPS memory/DB pool/Kafka lag bounds.
- Проверить frontend accessibility/responsive behavior, keyboard navigation,
  localization and screenshots for summary, channel matrix, issue drawer,
  remediation preview and approval state.
- Обновить product publication guide, rule/profile contract, API/event docs,
  connector capability matrix, operations/runbook and public documentation;
  generated SDK/catalogs must match runtime.
- Retain synthetic qualification bundle with profile/ruleset/snapshot digests;
  no production PII, tokens, raw provider responses or unreviewed screenshots.

**Acceptance:** `go test ./...`, `go vet ./...`, contracts, architecture,
migrations, frontend, conformance, performance and Compose E2E checks pass;
documentation/UI/runtime are consistent, and no publication capability is
promoted without current quality + connector evidence.

## Suggested delivery slices

1. **Quality foundation:** 166.1–166.4 — ADR, model, profile schema and
   canonical snapshot assembly.
2. **Decision and runtime gate:** 166.5–166.8 — deterministic engine, score,
   preflight gate, worker, persistence, RLS and lineage.
3. **Operator workflow:** 166.9–166.12 — API/UI, remediation/approval,
   connector qualification, observability and recovery.
4. **Release qualification:** 166.13 — tests, Docker/Compose, load/chaos,
   screenshots, docs and retained evidence.

## Explicit exclusions

- Quality score is not a sales/conversion guarantee and cannot override hard
  blockers, compliance, capability, stale evidence or server authorization.
- No provider-specific fields, branches, private APIs, browser scraping,
  arbitrary code/SQL/HTTP, unbounded regex/evaluation or direct secret access.
- No automatic rewriting of Product/PIM/media/compliance facts by AI; generated
  content remains a reviewed suggestion and uses normal mutation/approval APIs.
- No remote write before exact quality receipt + existing compliance guard;
  no blind retry after remote accepted/unknown, no false published status and
  no quality decision from ClickHouse/cache/browser state.
- No promotion of SDK-only, health-only, not-configured or stale connector
  profile; unsupported operation stays explicitly `unsupported`.
- No destructive PIM merge, automatic price/stock changes, order/return/refund
  execution or purchase-order action; those remain their own domain workflows.

## References

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/02-domain-model.md`
- `docs/03-module-boundaries.md`
- `docs/04-event-platform-kafka.md`
- `docs/05-database.md`
- `docs/06-api.md`
- `docs/08-sync-reconciliation.md`
- `docs/10-integrations-matrix.md`
- `docs/20-schema-registry.md`
- `docs/19-pim-mdm.md`
- `docs/29-data-lineage.md`
- `docs/34-frontend.md`
- `docs/44-connector-conformance.md`
- `docs/46-sre-performance-slo.md`
- `docs/56-product-compliance.md`
- `docs/69-frontend-shell.md`
- `adr/0009-transactional-outbox.md`
- `adr/0018-slo-performance.md`
- `adr/0024-product-compliance-policy.md`
- `adr/0030-upload-quarantine.md`
- `adr/0041-approval-engine-policy-evidence.md`
- `adr/0090-built-in-provider-runtime-composition-boundary.md`
- `adr/0100-runtime-truthful-integration-catalog.md`
- `adr/0115-commerce-product-event-runtime-route.md`
- `tasks/issues/004-catalog-domain.md`
- `tasks/issues/023-pim-mdm.md`
- `tasks/issues/082-product-compliance-core.md`
- `tasks/issues/088b-upload-security-pipeline.md`
- `tasks/issues/104-integration-catalog-settings.md`
- `tasks/issues/119-ui-product-experience-closure.md`
- `tasks/issues/130-runtime-truthful-integration-catalog.md`
- `tasks/issues/161-commerce-product-event-runtime-route.md`
- `tasks/issues/163-workflow-automation-builder.md`
