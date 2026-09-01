# Task 230 — Массовое управление каталогом из единого интерфейса

## Статус

`repository-complete` — карточка товара, PIM, quality gate, изображения, channel
projection и отдельные pricing/repricing preview уже существуют в Tasks 221 и
222. Единый multi-channel workspace, где одной операцией можно безопасно
менять карточки, изображения, характеристики, цены и описания с различиями по
каждому каналу, реализован ниже.

Репозиторный контур Task 230 закрыт: реализованы единый immutable scope и
selection snapshot, сравнение channel projections, typed bulk changes для
контента/локализаций/категорий/характеристик/вариантов/media/цен/остатков,
quality/compliance и capability gates, preview/diff, approval-bound partial
apply, cursor history, actor/audit evidence, reconciliation, RLS, quotas,
recovery и tenant-scoped kill switch. Добавлены REST/OpenAPI, Go/Python/
TypeScript SDK, MCP dry-run, frontend workspace, synthetic qualification,
E2E/contract fixtures и операционная документация. Реальные credentialed
записи и read-after-write каждого marketplace остаются внешним release-gate;
неподтверждённые каналы не получают статус `qualified`.

## Цель

Дать оператору единый массовый процесс:

```text
выбор товаров/каналов → сравнение состояний → массовое редактирование
→ channel validation/quality gate → preview diff → approval
→ apply по каналам → частичные результаты → read-after-write/reconciliation
```

Общие поля должны редактироваться один раз, а channel-specific поля — в той
же рабочей области с явным mapping, requirement и ограничениями канала.
Изменения не должны загрязнять canonical PIM Product/Offer полями конкретного
marketplace. Невалидные строки не отправляются, успешная одна строка не
маскирует ошибку другой.

## Что уже есть и что закрывает этот task

- Task 222 даёт карточку, PIM, taxonomy/attribute mapping, localized content,
  images, variants, quality preflight и базовый batch engine.
- Task 221 даёт pricing preview, floor/max-step guards и подготовку
  безопасного массового изменения цен.
- Task 225 даёт отдельный approval-bound контур акций и рекламы.
- Task 217 даёт versioned publication snapshot и publication/reconciliation
  state.

Task 230 не создаёт второй каталог, price ledger или publication lifecycle.
Он собирает эти возможности в один операторский bulk workspace и закрывает
cross-channel selection, diff, orchestration, partial apply и recovery.

## Target end-to-end slice

Для выбранных товаров и двух подключённых каналов оператор должен пройти:

1. выбрать товары по SKU, категории, каналу, кабинету, статусу, quality или
   saved filter;
2. увидеть common fields и отличия channel projections: category, attributes,
   variants, localized title/description, images, price и availability;
3. применить typed mass operation к полю/группе полей или загрузить
   подготовленный CSV/XLSX template с preview mapping;
4. получить по строкам validation, floor/quality/compliance blockers, conflicts
   и ожидаемую remote impact;
5. подтвердить только разрешённые строки через approval/Idempotency-Key;
6. выполнить apply отдельными bounded jobs по channel/account и получить
   `applied`, `rejected`, `unknown`, `skipped` и `manual_attention`;
7. увидеть read-after-write, drift, audit diff и безопасный retry/reconcile.

## Подзадачи

### 230.1 — ADR и общий контракт массовой операции

**Зависимости:** 221, 222, 225, 217.

- Зафиксировать ownership: Product/PIM, Offer/price, channel projection,
  taxonomy/mapping, media, quality, publication и reconciliation.
- Определить common field, channel-specific field, channel override, derived
  field, read-only remote field и conflict semantics.
- Ввести typed `BulkCatalogOperation` с scope, selection snapshot, field paths,
  operation kind (`set`, `replace`, `append`, `remove`, `normalize`, `copy`),
  rule version, actor и approval.
- Определить lifecycle `draft → previewed → awaiting_approval → queued →
  running → partial/completed/failed/cancelled` и per-row state.
- Утвердить, какие операции auto-safe, требуют approval или запрещены; mass
  clear/delete всегда отдельная явная команда с recovery evidence.

**Acceptance:** ADR не допускает смешения PIM truth с channel override;
каждая операция имеет owner, risk, rollback, idempotency, quota и источник
истины; status batch не скрывает per-row failures.

### 230.2 — Выбор товаров, каналов и единый bulk scope

**Зависимости:** 230.1.

- Поддержать selection по SKU, Offer, category, brand, warehouse, channel,
  account, publication status, quality blocker, image status и saved filter.
- Разделить `selected PIM products` и `selected channel projections`, чтобы
  одна команда могла менять общий контент или только конкретный канал.
- Ограничить scope workspace/account/permission, maximum SKU/fields/bytes и
  duration; выбор «все результаты фильтра» должен сохранять immutable snapshot.
- Добавить preview count и предупреждение о каналах без нужной capability,
  stale taxonomy или отсутствующем mapping.
- Поддержать CSV/XLSX import template only after released upload, schema
  preview, column mapping и validation; не принимать свободные provider JSON.

**Acceptance:** одинаковый filter digest даёт одинаковый набор; cross-tenant,
  archived product, hidden channel and stale selection fail closed; повтор
  selection не меняет состав batch незаметно.

### 230.3 — Multi-channel projection и field mapping workspace

**Зависимости:** 222.2–222.3, 230.1–230.2.

- Показать одну common model рядом с channel-specific taxonomy, attribute,
  category, locale, media-slot и validation requirements.
- Разрешить mapping PIM → channel field с transform/unit/enum/fallback,
  версией схемы, source, freshness и owner.
- Отображать differences: local draft, published projection, remote observed,
  channel override и конфликт версий.
- Не смешивать taxonomy/constraints разных каналов и не применять mapping,
  если fingerprint схемы устарел.
- Поддержать copy mapping/rule только через explicit preview и approval для
  затронутых channel projections.

**Acceptance:** оператор видит, какие значения общие, а какие channel-specific;
один invalid mapping блокирует только соответствующие строки, а PIM snapshot
остаётся неизменным до подтверждённого действия.

### 230.4 — Массовое редактирование описаний и локализаций

**Зависимости:** 222.4, 230.2–230.3.

- Поддержать title, description, bullets, SEO, brand text, composition,
  instructions, warnings и channel-specific content variants.
- Реализовать locale fallback, translate/copy, append/replace/normalize,
  version history, source vs override и compare before/after.
- Валидировать длину, язык, forbidden terms, markup, required phrase,
  marketplace formatting и moderation до preview/apply.
- Разделить public content, internal note и AI-generated draft; AI не получает
  право publish или обходить approval/quality.
- Сохранять provenance, author, rule version, source snapshot и rollback
  reference для каждого изменённого поля.

**Acceptance:** массовый title/description update по двум locales и двум
каналам выдаёт per-row diff; invalid length/markup/prohibited term блокирует
строку, а approved previous content можно восстановить новой операцией.

### 230.5 — Массовые характеристики, категории и варианты

**Зависимости:** 222.2–222.3, 222.6, 230.3.

- Массово менять category, required/conditional attributes, enum/unit values,
  variant axes, parent/child links, barcode/GTIN references и product facts.
- Сохранять channel-specific category/attribute mapping отдельно от canonical
  Product и проверять exact taxonomy version.
- Поддержать typed set/replace/append/remove для multi-value fields; explicit
  clear требует отдельного подтверждения.
- Валидировать variant uniqueness, orphan child, required offer/price/stock,
  incompatible enum/unit and identity change blast radius.
- Формировать quality/compliance blockers с ссылкой на конкретный SKU/field.

**Acceptance:** category/attribute/variant batch на 1 000 SKU не применяет
невалидные строки; повтор не создаёт дублей вариантов, PIM identity не меняется
без approval, а channel projections сохраняют свои taxonomy rules.

### 230.6 — Массовое управление изображениями и media slots

**Зависимости:** 222.5, 230.2–230.3.

- Поддержать upload/replace/remove/reorder/main-image и привязку image asset к
  channel media slots; original PIM asset не удалять при channel remove.
- Добавить library selection, массовую загрузку, mapping по SKU/filename и
  preview для main/gallery/detail/size-chart/certificate.
- Проверять MIME, dimensions, aspect ratio, size, duplicate, alt text,
  quarantine/release, channel slot count и quality requirements.
- Показывать progress, failed upload, retry, skipped unsupported format и
  partial result; failed asset не должен стать published.
- При изменении media после preview инвалидировать старый quality/publication
  receipt и сохранять immutable asset digest.

**Acceptance:** batch image operation с 1 000 SKU имеет per-row status,
main thumbnail, remove/replace/reorder, retry и failure state; повтор upload не
создаёт дубль и не удаляет исходный PIM asset.

### 230.7 — Массовые цены, предложения и остатки

**Зависимости:** 221, 222.6, 230.2–230.3.

- Встроить pricing preview Task 221 в общий bulk workspace, сохраняя его
  floor-price, minimum-margin, max-step, currency и unit-economics guards.
- Различать edit Offer/PIM price, channel price projection, stock/availability
  и promotion price; не менять цену акции скрытой командой.
- Перед apply перечитывать текущую версию Offer, stock, floor/margin,
  qualification и remote state; stale preview блокировать.
- Показывать price diff, expected margin, affected SKU/channel/account,
  blocked rows и approval blast radius рядом с content/media changes.
- Поддержать atomicity только внутри локального batch; remote channels
  применяются отдельными idempotent jobs и могут дать partial result.

**Acceptance:** массовая смена цены не обходит Task 221 guards, не смешивает
валюты и не создаёт двойную запись; rejected/unknown price rows видны в общем
bulk result и не объявляются применёнными из-за успешного content apply.

### 230.8 — Общий quality, compliance и dependency gate

**Зависимости:** 230.4–230.7, 166, 217.

- Запускать quality preflight по каждому channel projection после применения
  draft changes, но до любого remote side effect.
- Объединять blockers из required attributes, stale taxonomy, content/media,
  variant, price/floor, compliance evidence, publication status и capability.
- Разделять hard blocker, warning, not applicable, unsupported и unknown;
  объяснение должно содержать field/asset/SKU/channel remediation.
- Инвалидировать preview при изменении PIM, Offer, taxonomy, image release,
  quality/compliance, price или mapping version.
- Хранить immutable quality receipt и связывать его с bulk input digest,
  policy, connector capability и snapshot version.

**Acceptance:** ни одна строка с hard blocker не попадает в apply; warning
сохраняется; quality result одинаков в UI, API, worker и publication path;
compliance нельзя обойти массовым импортом.

### 230.9 — Preview, diff, approval и batch orchestration

**Зависимости:** 230.1–230.8.

- Формировать dry-run с before/after diff по field path, asset digest,
  category/attribute/variant/price, channel/account и expected impact.
- Поддержать approve selected valid rows, reject/skip blocked rows, cancel,
  retry failed, reconcile unknown и rollback через новую версию.
- Partition jobs по channel/account/capability и сохранять order-independent
  stable digest; bounded batch size, bytes, fields, calls и concurrency.
- Применять `Idempotency-Key`, expected version, approval reference и
  deterministic remote idempotency key для каждой partition.
- Нормализовать `applied`, `rejected`, `conflict`, `unknown`, `skipped`,
  `manual_attention`; общий batch не может скрыть partial outcome.

**Acceptance:** 1 000 SKU × 2 channels проходит bounded preview; duplicate
  apply, worker crash, timeout after remote accept, out-of-order webhook,
  concurrent edit, approval expiry и rate limit не создают двойного эффекта.

### 230.10 — Durable persistence, API, OpenAPI и SDK

**Зависимости:** 230.2–230.9.

- Добавить expand-only storage для bulk selection snapshot, operation, field
  changes, asset refs, preview/diff, approval, partition/job, row result,
  quality receipt, remote receipt and reconciliation.
- Включить FORCE RLS, idempotency uniqueness, optimistic version, append-only
  audit/history, bounded indexes and retention/legal hold.
- Добавить cursor API для bulk runs, preview, diff, row results, channel
  projection, retry/reconcile/rollback and import validation.
- Обновить OpenAPI, Go/TypeScript/Python SDK, events and capability/permission
  labels; stable codes сохранить, UI-статусы русифицировать.
- Safe errors должны возвращать code, field/SKU/channel path, correlation ID,
  retryability and remediation без raw provider payload.

**Acceptance:** contract parity, migration checks, RLS/tenant negative tests,
SDK drift, pagination, replay and append-only tests pass; API не превращает
частичный результат в общий success.

### 230.11 — Единый UI «Массовый каталог»

**Зависимости:** 230.3–230.10.

- Сделать одну страницу с шагами «Выбор», «Сравнение каналов», «Изменения»,
  «Проверка», «Согласование», «Применение» и «История».
- Дать общую таблицу с колонками SKU, изображение, название, канал, категория,
  атрибуты, варианты, цена, quality, diff и status; раскрывать channel details.
- Поддержать inline/spreadsheet edit, copy from source/channel, templates,
  bulk image actions, price preview и safe clear confirmation.
- Для каждой строки показать exact blocker, source/override, taxonomy version,
  freshness, remote receipt, retry/reconcile и ответственное действие.
- Не показывать кнопку, если отсутствует capability/permission/approval;
  `Нет поддержки`, `Ошибка`, `Нужна квалификация` и `Неизвестно` различать.

**Acceptance:** authenticated browser tests покрывают 320–1440px, keyboard/
focus, 1 000-row virtualized table, filter selection, image preview/failure,
attribute/content/price edit, approval, partial apply, retry и history.

### 230.12 — Connector capability и read-after-write qualification

**Зависимости:** 226, 230.5–230.10.

- Составить matrix per channel/account для product/content, attributes,
  categories, variants, media, prices, inventory и publication operations.
- Для каждой capability проверить official API, scopes, rate limits, partial
  update/clear semantics, idempotency, async processing, webhook и
  read-after-write.
- Квалифицировать первой волной минимум два channel connectors с разными
  taxonomy/media/price constraints; не предполагать одинаковую семантику.
- Синхронизировать manifest, runtime guard, API, UI, worker и MCP; manifest/
  SDK наличие не включает массовую запись.
- Зафиксировать fallback read-only и next action для unsupported channels.

**Acceptance:** connector conformance evidence привязано к версии и дате;
каждый enabled bulk write имеет exact field/asset/price evidence, а unknown
remote result создаёт reconciliation task.

### 230.13 — Audit, observability, quotas и recovery

**Зависимости:** 230.9–230.12.

- Аудировать actor, selected filter digest, before/after field/asset/price
  digest, rule/mapping/taxonomy version, approval, partition, receipt и result.
- Метрики: selected/changed/blocked SKU, preview/apply latency, error/unknown,
  media bytes/failures, quality blockers, stale data, remote calls, queue lag и
  reconciliation age.
- Добавить quotas per workspace/account: SKU, fields, image bytes, price delta,
  remote calls, concurrent jobs, exports and retry budget.
- Ввести kill switch для mass content/media/price/publication writes и runbooks
  для timeout, partial apply, bad mapping, wrong image, stale taxonomy и
  accidental clear.
- Задать retention для previews/diffs, published snapshots, audit и released
  media; legal hold имеет приоритет.

**Acceptance:** quota isolation, kill switch, redacted logs, rollback/recovery,
cross-tenant and destructive-operation tests pass; остановка записи не удаляет
evidence и не ломает PIM truth.

### 230.14 — Demo, E2E, load и release gate

**Зависимости:** все предыдущие подзадачи.

- Добавить synthetic catalog минимум из 1 000 SKU с разными категориями,
  variants, localized descriptions, valid/invalid images, required attributes,
  prices, floor violations и двумя channel taxonomies.
- Пройти один общий сценарий: выбор → content/image/attribute/variant/price
  edit → quality → preview → approval → partition apply → read-after-write →
  partial failure → retry/reconcile.
- Проверить stale taxonomy, invalid enum/unit, missing image, upload failure,
  price floor, concurrent edit, duplicate batch, timeout after remote accept,
  rate limit, unsupported capability, cross-tenant and approval denial.
- Добавить authenticated browser E2E и Compose API/worker E2E с synthetic
  channel connectors; нагрузочный smoke — минимум 1 000 SKU batch runs.
- Release разрешает массовую запись только для connector-ов с актуальным
  conformance/live or sandbox evidence; остальные остаются read-only.

**Acceptance:** один интерфейс обрабатывает карточки, изображения,
характеристики, цены и описания; частичный результат не теряется, дублей нет,
PIM snapshot и история публикации неизменны, production claim подтверждён
retained evidence.

## Архитектурные ограничения

- PIM Product/Offer остаются canonical truth; channel-specific projection,
  mapping, override и publication snapshot — отдельные versioned records.
- PostgreSQL — transactional source; Outbox/Inbox, idempotency и lease fencing
  защищают workers; browser state не является источником истины.
- Core не ветвится по marketplace name; channel differences живут в typed
  capabilities, schemas and adapters.
- Все операции tenant/workspace-scoped, write-sensitive и approval/policy/
  quota-gated; неизвестный remote result не считается применённым.
- Raw provider payload, secrets, customer PII и untrusted HTML/scripts не
  сохраняются в catalog rows, events, logs, fixtures или exports.
- AI/MCP/n8n могут формировать draft/preview, но не могут сами применить batch,
  одобрить свою операцию или обойти quality/floor/compliance.

## Не входит в этот task

- Новый PIM master, marketplace taxonomy engine или pricing algorithm.
- Repricing/Buy Box/advertising/promotion logic как самостоятельные домены;
  используется Task 221/225.
- Scraping и undocumented marketplace endpoints.
- Бухгалтерский/складской ledger и самостоятельное автоматическое разрешение
  остатков.

## Зависимости

166, 217, 221, 222, 225, 226.

## Definition of Done

- Все 14 подзадач имеют implementation, contracts/docs и success/failure/
  idempotency tests.
- В одном интерфейсе доступны mass edit карточек, изображений, атрибутов,
  вариантов, цен и описаний по нескольким каналам.
- Preview/diff, quality, approval, per-row results, retry, read-after-write,
  reconciliation, RLS, audit, quotas и kill switch работают согласованно.
- Для каждой массовой write capability в репозитории есть synthetic или
  sandbox boundary evidence; unsupported channels остаются
  read-only/qualification_required. Credentialed marketplace evidence
  заполняется на целевой topology перед release.
- Пройдены `gofmt`, `go test ./...`, `go vet ./...`,
  `./scripts/check-contracts.sh`, `make migrations`,
  frontend typecheck/build и `make mass-catalog-qualification`. Общий
  `make architecture` остаётся отдельным legacy-gate: он сейчас сообщает о
  старых ADR/review и provider-ветках вне Task 230; собственный ADR/review и
  новые catalogbulk-модули соответствуют архитектурному контракту.
