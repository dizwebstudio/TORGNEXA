# Task 222 — Marketplace-карточки: атрибуты, контент и массовое редактирование

## Status

`repository-complete` — the provider-neutral repository slice is available
through the existing Catalog/PIM, Publication Quality and Marketplace
Publication frontend surfaces. Operators can edit title/description, offers
and prices, categories, images and publication snapshots, run the quality
preflight and inspect approval/operation/reconciliation state. The canonical
PIM/catalog records remain the source of truth; provider-specific taxonomy,
attribute mapping, 1,000-SKU batch apply and live channel writes remain
qualification-gated and are not represented as finished capabilities.

## Objective

## Repository completion evidence — 2026-09-01

- `/catalog` provides the operator card for product identity, localized text,
  offers/SKU, prices, categories and released media, with optimistic versions
  and safe image validation;
- `/publication-quality` exposes readiness, blockers and remediation state;
  `/marketplace-publications` provides snapshot preflight, dry-run/live guard,
  approval reference, operation status and reconciliation drift;
- the existing PIM, media, quality and publication contracts remain separate;
  the implementation does not add provider-specific fields to canonical
  Product truth or expose raw provider payloads;
- the task documentation now distinguishes repository functionality from the
  later connector taxonomy, batch-edit and live read-after-write gate.

This closes the repository content/publication slice. The remaining items are
external qualification gates and must be admitted only after official
taxonomy/attribute contracts, batch evidence, approval and connector
read-after-write checks are available.

Сделать полноценную карточку товара для каждого подключённого канала без
дублирования Product truth. Оператор должен иметь возможность выбрать категорию
канала, заполнить обязательные и условные атрибуты, собрать варианты/SKU,
подготовить заголовок, описание, характеристики и изображения, увидеть ошибки
до публикации и применить изменения сразу к группе товаров.

Marketplace projection должна быть versioned и channel-specific: различия
названий полей, типов, единиц, ограничений длины, media slots, категорий,
вариантов и статусов остаются на границе connector/PIM adapter. Общая карточка
PIM не должна загрязняться provider-specific полями.

## Target end-to-end slice

Для одного товара и batch из 1 000 SKU должен проходить сценарий:

1. загрузить актуальную taxonomy канала и выбрать категорию;
2. сопоставить PIM-поля с обязательными/условными channel attributes,
   включая тип, enum, единицу измерения и правила валидации;
3. подготовить локализованные title, description, bullet points, brand,
   dimensions, variants и изображения с понятным preview;
4. запустить quality preflight и получить построчные blockers/warnings:
   missing attribute, invalid enum, bad unit, missing image, stale taxonomy,
   compliance evidence или unsupported capability;
5. отредактировать одну или несколько карточек массовым правилом через dry-run
   preview с diff, затронутыми SKU и оценкой результата;
6. получить approval для массовой записи, изменения идентичности товара,
   контента с высоким blast radius или публикации;
7. опубликовать только валидные projections через typed connector capability,
   получить read-after-write и сохранить receipt/version;
8. обработать partial/rejected/unknown результат без ложного успеха и показать
   оператору конкретное исправление и безопасный retry.

### Definition of done

Для одного полностью квалифицированного marketplace выполняются create/update
карточки, category mapping, required attributes, variants, media, localized
content, batch edit и publication status. Preview на 1 000 SKU детерминирован,
невалидные строки не уходят в remote write, повтор операции не создаёт дубль,
а исходный PIM snapshot и история публикации сохраняются неизменными.

## Architecture boundaries

- `Product`, `Offer` и PIM facts остаются каноническим источником; listing
  projection, mapping и channel content — отдельные versioned records.
- Core не ветвится по marketplace ID и не знает provider-specific JSON. Для
  этого используются typed taxonomy, mapping и listing connector ports.
- Таксономия, схема атрибутов и ограничения имеют `source`, `version`,
  `observed_at` и freshness. Устаревшая схема не допускает live publish.
- Значения атрибутов типизированы: text, enum, multi-enum, integer, decimal,
  boolean, dimension, weight, date и media reference. Float для денег и
  количеств не используется.
- AI может предложить content/attribute mapping только как draft с provenance;
  AI не публикует карточку и не обходит quality, compliance, policy или approval.
- Массовая запись — `write_sensitive`: preview, limits, idempotency,
  optimistic version, approval и reconciliation обязательны.
- Неизвестный remote result не переводится в `published`; raw provider payload,
  секреты и customer PII не сохраняются в обычной модели, events или logs.

## Subtasks and implementation order

### 222.1 — ADR, channel listing model and ownership matrix

**Depends on:** none.

- Зафиксировать ADR и границы: Product/PIM truth, Offer, channel projection,
  taxonomy, attribute mapping, content variant, media slot и publication receipt.
- Определить ownership каждого поля и поведение при конфликте PIM vs channel;
  ручной channel override должен быть явным, версионным и аудируемым.
- Утвердить lifecycle projection: `draft → ready → queued → processing →
  published|rejected|unknown|manual_attention`.
- Определить, какие изменения auto-safe, какие требуют approval: identity,
  category, brand, compliance, price, media bulk, content bulk и delete/archive.
- Согласовать совместимость с Tasks 166 (quality), 217 (publication), 167
  (unit economics), catalog/PIM, images, compliance и approval.

**Acceptance:** ownership matrix и state transitions одобрены; нет поля,
которое одновременно редактируется как PIM truth и как независимый channel
override без правила разрешения конфликта.

### 222.2 — Taxonomy and attribute schema contracts

**Depends on:** 222.1.

- Ввести typed contracts для category tree, attribute definition, requirement
  condition, enum values, units, media slots, variants and validation rules.
- Поддержать required/optional/conditional attributes, mutually dependent
  fields, allowed values, min/max, regex, decimal scale and localized labels.
- Добавить taxonomy import/read через connector port с version/fingerprint,
  source, observed_at, locale и jurisdiction.
- Нормализовать единицы измерения, размеры, вес, composition, color, gender,
  age group и variant axes без потери исходной точности.
- Не смешивать taxonomy version одного канала с другим; mapping должен ссылаться
  на exact source version.

**Acceptance:** contract/domain tests покрывают типы, units, conditional rules,
enum deprecation, duplicate values, locale, stale schema and taxonomy diff.

### 222.3 — Versioned mapping and transformation engine

**Depends on:** 222.2.

- Реализовать mapping PIM field → channel attribute с transform chain:
  rename, enum map, unit conversion, format, composition and fallback.
- Добавить manual override с reason, actor, version и source; autogenerated
  suggestions должны быть отделены от accepted mapping.
- Поддержать mapping для parent/child/variant SKU, bundles, manufacturer,
  barcode/GTIN и channel-specific identifiers.
- Проверять mapping против exact taxonomy fingerprint и запрещать silent remap
  после изменения схемы.
- Возвращать typed diagnostics с path, severity, source field, target field and
  remediation text; raw provider error наружу не отдавать.

**Acceptance:** deterministic mapping tests на valid/invalid/ambiguous values,
unit conversion, stale mapping, variant conflict, missing source and mapping
version mismatch; одинаковый snapshot даёт одинаковый digest.

### 222.4 — Content and localization workspace

**Depends on:** 222.1–222.3.

- Создать versioned content variants для title, description, bullets, SEO,
  brand text, composition, instructions and warnings per channel/locale.
- Валидировать длину, запрещённые символы/слова, HTML/markup, required terms,
  language and channel formatting before preview.
- Добавить compare source/draft/published, author, provenance, created_at,
  approval state and rollback to prior draft; published content immutable.
- AI generation и translation сохраняют model/provider, prompt digest,
  source references and confidence, но результат всегда остаётся draft.
- Связать claims, certificates and compliance evidence с content field, если
  канал требует подтверждения.

**Acceptance:** tests на locale fallback, length/markup, prohibited terms,
content conflict, AI draft provenance, approval and immutable publish history.

### 222.5 — Media slots and asset validation

**Depends on:** 222.1–222.3.

- Сопоставить product images/video/documents с channel media slots: main,
  gallery, detail, size chart, certificate and rich content.
- Проверять MIME, dimensions, file size, aspect ratio, background, position,
  alt text, duplicate asset and upload/release/quarantine state.
- Добавить thumbnail, upload failure, retry, remove/replace and ordering
  semantics; удаление channel asset не должно удалять PIM original.
- Хранить immutable asset digest and release reference; channel projection
  сохраняет only safe remote receipt and URL/reference policy.
- Quality gate должен отличать `missing`, `invalid`, `upload_failed`, `stale`
  и `not_supported`.

**Acceptance:** browser/API tests cover first/main image, failed upload, retry,
remove/replace, ordering, unsupported video, quarantine and stale asset.

### 222.6 — Variants, offers and catalog consistency

**Depends on:** 222.2–222.5.

- Реализовать channel-specific variant axes and parent/child projection without
  changing canonical Offer/SKU identity.
- Проверять completeness: every child has required price, stock, barcode,
  media and attributes; no duplicate variant combination.
- Поддержать partial update without deleting fields absent from a request;
  explicit clear must be a separate typed operation.
- Reconcile local projection against remote listing for title, category,
  attributes, variants, media, price and availability.
- Emit actionable drift findings and block publication when identity or variant
  mapping is ambiguous.

**Acceptance:** full/partial variant set, duplicate axes, orphan child, remote
drift, partial update, explicit clear and concurrent offer version tests pass.

### 222.7 — Quality preflight and publication gate integration

**Depends on:** 222.3–222.6.

- Расширить Task 166 profile rules для category/attributes/content/variants/
  media, including score, blockers, warnings, freshness and evidence.
- Собрать immutable listing snapshot with product, offer, mapping, taxonomy,
  media, compliance and connector capability versions.
- Block `products.write`/publication on missing required field, stale taxonomy,
  invalid mapping, failed media release, unsupported channel attribute or
  unknown compliance.
- Explain every blocker in Russian and provide remediation link to the exact
  field/asset/mapping.
- После изменения source facts старый quality receipt должен стать stale;
  receipt from another product/offer/channel must not be reusable.

**Acceptance:** preflight catches each blocker, preserves warnings, invalidates
old receipt after source change and agrees with publication worker/API/UI.

### 222.8 — Mass edit preview and batch command engine

**Depends on:** 222.3–222.7.

- Поддержать selection by filter/category/channel/status/SKU and typed batch
  operations: set, replace, map, append, remove, normalize and copy.
- Preview должен содержать affected count, per-row before/after diff,
  validation result, expected publication impact, blocked rows and conflicts.
- Add dry-run digest, immutable input snapshot, rule version, batch partitioning,
  max affected SKU, max changed fields and per-day quota.
- Запретить массовое удаление/очистку по умолчанию; destructive clear требует
  explicit confirmation and approval.
- При частичном результате сохранять per-row status; нельзя показать batch как
  успешно завершённый при наличии unknown/rejected строк.

**Acceptance:** preview 1 000 SKU is bounded/deterministic; filters do not cross
  tenant, blocked rows are excluded, repeated command is idempotent and diff
  remains available after apply.

### 222.9 — Persistence, RLS, audit and lineage

**Depends on:** 222.2–222.8.

- Add expand-only PostgreSQL migration for taxonomy versions, attribute schemas,
  mappings, content variants, media slots, listing projections, batch runs,
  candidates, receipts, drift and validation evidence.
- Enable `FORCE ROW LEVEL SECURITY`, organization/workspace predicates,
  optimistic versions, idempotency uniqueness and bounded indexes.
- Keep published snapshots, mapping changes, mass-edit diffs and approval/audit
  evidence append-only; raw provider payloads are excluded.
- Define retention for high-frequency taxonomy/observation cache vs published
  listing history, compliance and audit evidence.
- Link each projection/receipt to product, offer, connector account, taxonomy
  fingerprint and snapshot digest.

**Acceptance:** migration static checks, fresh/upgrade rehearsal, two-tenant RLS,
cross-tenant negative tests, append-only and idempotency tests pass.

### 222.10 — Connector runtime and qualification matrix

**Depends on:** 222.2–222.7.

- Add typed ports/capabilities for taxonomy read, listing read, category mapping,
  attributes write, content write, media write and publication status.
- Separate `products.write` from future listing-specific capabilities only when
  the provider contract requires it; stable codes and risk policy are host-owned.
- Qualify one provider first for taxonomy, required attributes, content, media,
  variants, create/update, read-after-write and rejected/unknown outcomes.
- Record provider rate limits, field constraints, remote partial-update semantics
  and webhook/reconciliation behavior outside Core.
- Keep unsupported operations visibly `qualification_required` or
  `not_available`; SDK/manifest presence alone cannot enable them.

**Acceptance:** generated capability catalog equals runtime support; every
enabled write has deterministic fixtures and current Docker/live evidence;
unsupported operation is denied through API, UI, worker and MCP.

### 222.11 — API, OpenAPI, SDK and AI/MCP boundaries

**Depends on:** 222.7–222.10.

- Add routes for taxonomy/category, mapping, attribute validation, content
  variants, media slots, listing projection, batch preview/apply and drift.
- Every mutation requires `Idempotency-Key`, expected version and authenticated
  organization/workspace scope; lists use cursor pagination.
- Update OpenAPI, generated Go/TypeScript SDK, events, permissions and Russian
  labels while preserving stable technical codes.
- MCP/OpenClaw may request preview and draft generation but cannot apply listing
  writes, approve its own request or bypass quality/approval/quotas.
- Normalize errors into missing/invalid/stale/conflict/unsupported/unknown with
  field-level remediation data.

**Acceptance:** contract parity, tenant isolation, cursor pagination, retry,
  permission checks, safe error payloads and MCP denial tests pass.

### 222.12 — Operator UI and product-card workflow

**Depends on:** 222.4–222.11.

- Добавить в карточку товара вкладки/блоки: «Категория канала», «Атрибуты»,
  «Контент», «Варианты», «Изображения», «Проверка» и «История публикации».
- Показывать required/conditional fields, enum/unit controls, source vs override,
  taxonomy version, freshness and exact remediation.
- Сделать bulk editor with filters, spreadsheet-like safe edits, preview diff,
  select valid rows, approval step, progress, partial results and retry.
- Для изображения обязательно показывать main thumbnail, upload progress,
  failed state, remove/replace and channel-specific validation.
- Не скрывать provider limitations: `Не поддерживается каналом` отличается от
  `Ошибка загрузки` и `Нет прав`.

**Acceptance:** keyboard/focus, responsive layout, 1 000-row preview, create/
  update media, attribute validation, approval, error/retry and publication
  timeline browser tests pass.

### 222.13 — Security, observability, quotas and recovery

**Depends on:** 222.8–222.12.

- Metrics: taxonomy freshness, mapping errors, missing attributes, content
  blockers, media failures, batch size, apply latency, rejected/unknown rows,
  drift, retry/DLQ and approval wait.
- Quotas by workspace/account: taxonomy calls, content generation, media bytes,
  affected SKU, changed fields, publication calls and concurrent runs.
- Audit actor, before/after digest, field paths, rule/mapping version, approval,
  remote receipt and result; redact PII and provider payloads.
- Kill switch for mass content/media/listing writes; pause new side effects but
  retain evidence and allow reconciliation/manual resolution.
- Runbooks for stale taxonomy, rejected attribute, failed upload, partial batch,
  remote accepted/local timeout, wrong category, drift and rollback to prior
  projection.

**Acceptance:** quota isolation, kill switch, redacted logs, incident alerts,
manual recovery and no-silent-data-loss tests pass.

### 222.14 — Demo data, Compose E2E, load and release qualification

**Depends on:** all previous subtasks.

- Add synthetic taxonomy with required/conditional attributes, enum/unit rules,
  variants, localized content, valid/invalid images and publication outcomes.
- Run one product and 1 000 SKU batch through category → mapping → content →
  media → quality → preview → approval → publish → read-after-write → drift.
- Cover stale taxonomy, missing field, invalid enum, failed upload, AI draft,
  concurrent edit, duplicate batch, partial remote result, timeout, rate-limit,
  out-of-order webhook and connector unsupported capability.
- Add screenshots, operator docs, API/events docs, mapping guide, qualification
  matrix and migration/backup instructions.
- Release gate requires one marketplace with current evidence for attributes,
  content, media, variants and mass update; other channels remain explicit
  `qualification_required`.

**Acceptance:** `go test ./...`, `go vet ./...`, contract/architecture/migration
checks, frontend typecheck/build, connector conformance and Compose E2E pass;
no production PII appears in fixtures or screenshots.

## Suggested delivery slices

1. **Card foundation:** 222.1–222.3 — ownership, taxonomy and mapping.
2. **Content completeness:** 222.4–222.7 — content, media, variants and quality.
3. **Mass editing:** 222.8–222.9 — preview, batch commands and durable evidence.
4. **First production channel:** 222.10–222.11 — connector, API and SDK.
5. **Operator experience:** 222.12–222.13 — UI, quotas, monitoring and recovery.
6. **Release gate:** 222.14 — demo, E2E, load, conformance and documentation.

## Explicit exclusions

- provider-specific fields or branches in Core;
- browser scraping, undocumented marketplace endpoints and bypassing rate limits;
- AI direct publish, automatic acceptance of generated claims or hidden content
  replacement;
- mass clear/delete without explicit operation, approval and recovery evidence;
- publication with stale taxonomy, missing required evidence, unknown media
  release or ambiguous mapping;
- treating SDK/manifest presence or a successful request acknowledgement as proof
  that the listing is published;
- raw provider payloads, credentials, customer PII or unbounded HTML/scripts in
  listing snapshots and events.

## References

- `docs/00-product-scope.md`
- `docs/01-architecture.md`
- `docs/03-module-boundaries.md`
- `docs/37-growth-advertising-promotions.md`
- `docs/operations/217-marketplace-product-publication.md`
- `docs/operations/051-promotions-pricing-guards.md`
- `contracts/plugins/marketplace-listing-v1.schema.json`
- `tasks/issues/166-product-publication-quality-center.md`
- `tasks/issues/167-channel-unit-economics.md`
- `tasks/issues/217-marketplace-product-publication.md`
