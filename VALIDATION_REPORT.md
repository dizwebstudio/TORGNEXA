# Repository validation report — Tasks through 170 — 2026-08-30

## Task 170 follow-up repository validation

- 170.5: WMS task context keeps warehouse/SKU/location/exact quantity bounded;
  scans persist only SHA-256 barcode digests, location, quantity, actor and
  UTC time;
- 170.6: standalone receiving, put-away and cycle-count tasks use the same
  idempotent lifecycle and PostgreSQL task history; automatic on-hand
  consumption is not performed;
- 170.7: migration `000031` adds forced-RLS `pack_handoff` batches of up to 50
  completed pick tasks with immutable events and audit/outbox evidence; the
  handoff is local and does not claim marketplace shipment or label support;
- 170.9: Inventory → «Задания WMS» provides queue filters, task detail,
  scanning, lifecycle commands, standalone task creation and pack handoff;
- 170.12 repository checks: **PASS** for Go tests/vet, contracts, migrations,
  architecture, generated SDK drift, frontend typecheck/logic tests and
  repository diff hygiene. Environment-specific deployment, backup/restore
  and live marketplace qualification remain separate gates.

## Task 159 repository validation

- Google Gemini and Grok are visible in Settings → AI providers and the report
  AI selector;
- Gemini uses `x-goog-api-key` and `generateContent`; Grok uses xAI Chat
  Completions with a Bearer key;
- both providers expose only bounded non-streaming text completion through the
  governed AI egress path;
- migration, OpenAPI, manifests, generated catalogs, policy, docs, tests and
  conformance evidence are synchronized: **61 providers / 18 generic / 43
  separate / 0 planned**;
- Midjourney remains intentionally unavailable because its official policy
  disallows third-party automation and provides no general public API.

## OpenCart bridge extension validation

- reference OpenCart 4.x extension is present under
  `connectors/storefronts/opencart/extension/torgnexa`;
- all 12 PHP bridge files pass `php -l` in `php:8.3-cli-alpine`;
- `scripts/package-opencart-bridge.sh` produces a valid
  `torgnexa.ocmod.zip` with the OpenCart `install.json` payload. The filename
  is intentionally fixed because OpenCart derives the installed extension
  code from it;
- the bridge exposes the required health, catalog, variant, inventory and
  order routes, uses a bearer-token digest boundary, and keeps customer PII
  out of order projections;
- live OpenCart Docker qualification remains deployment evidence and requires
  a non-production OpenCart 4.x instance with the extension installed.

### Local Docker smoke (2026-08-29)

- `docker-compose.opencart-test.yml` builds OpenCart 4.1.0.4 and MariaDB with
  a pinned SHA-256 release archive, installs the bridge and seeds synthetic
  products/order data;
- `scripts/opencart-smoke.sh`: **PASS** — unauthorized/authorized health,
  product list and SKU lookup, price replay/conflict, inventory write/read,
  product create/replay, order list and status update;
- PHP 8.3 lint, OpenCart package `unzip -t` and Compose config: **PASS**;
- the stack is local-only and is removed with
  `docker compose -f docker-compose.opencart-test.yml down -v`; this evidence
  does not qualify a production seller shop.

## WooCommerce Docker smoke (2026-08-29)

- `docker-compose.woocommerce-test.yml` builds WordPress 6.8.2, WooCommerce
  9.8.5 and MariaDB, enables the official REST API v3 and creates a synthetic
  Consumer Key/Secret pair;
- Compose health now waits for the seeded `TORGNEXA-WOO-COFFEE` SKU instead of
  treating an empty freshly-installed REST catalog as ready;
- `scripts/woocommerce-smoke.sh`: **PASS** — unauthorized/authorized Basic
  Auth, product list/SKU lookup, product price and managed-stock update,
  order list/status update and refunds endpoint;
- HTTPS uses a disposable self-signed certificate only for local validation;
  the production transport must verify the remote certificate;
- the stack is removed with
  `docker compose -f docker-compose.woocommerce-test.yml down -v`; this is
  deployment evidence for the WooCommerce API surface, not a production store
  qualification.

## PrestaShop Webservice Docker smoke (2026-08-29)

- `docker-compose.prestashop-test.yml` builds the official
  `prestashop/prestashop:8.1-apache` image with MariaDB, enables the native
  Webservice API and seeds two synthetic products plus a least-privilege API
  key;
- `scripts/prestashop-smoke.sh`: **PASS** — unauthorized/authorized Basic
  Auth, product list and reference lookup, official plural JSON envelope for a
  single resource, XML product-price PATCH, StockAvailable PATCH/readback and
  orders resource reachability;
- connector unit coverage now accepts the plural JSON envelope returned by the
  real `/api/products/{id}`, `/api/combinations/{id}` and `/api/orders/{id}`
  endpoints while preserving emulator compatibility;
- the storefront and Webservice screenshots are embedded in public
  documentation and the full runbook is
  `docs/connectors/prestashop/docker-smoke.md`;
- the stack is removed with
  `docker compose -f docker-compose.prestashop-test.yml down -v`; this is local
  deployment evidence, not a production seller-shop qualification.

## Task 158 repository validation

- «Долями» виден в категории «Платежи» как branded card;
- credentials используют callback-scoped логин/пароль и mTLS certificate;
- health-check использует одноразовый сертификатный HTTP-клиент и HTTPS probe;
- runtime support: `separate_surface/finance/health_only`, без payment route;
- manifests, policy, generated catalogs, docs, review и conformance evidence
  согласованы: **59 manifests / 18 generic / 41 separate / 0 planned**;
- live payment qualification, fixtures и webhook signature verification не
  заявлены и остаются отдельным gate.

## Task 157 repository validation

- Lamoda and М.Видео are visible as branded cards in «Интеграции →
  Маркетплейсы»;
- both cards use tenant-scoped API-key enrollment and the shared bounded HTTPS
  catalog probe with an operator-supplied endpoint;
- runtime support is `separate_surface/marketplace/health_only` with zero
  operational capabilities and zero product sync directions;
- policy, manifests, generated Go/TypeScript catalogs, docs, conformance
  evidence and architecture reviews agree on **58 manifests / 18 generic /
  40 separate / 0 planned**;
- frontend tests/build, contract checks and Go validation are required before
  release; live partner qualification is intentionally not claimed.

## Task 156 repository validation

- all former 14 `planned` entries are grouped into four explicit category
  surfaces: classified/verticals (3), social (6), EDO (2) and government (3);
- generated runtime support is synchronized at **58 manifests / 18 generic /
  40 separate / 0 planned**;
- every new card supports tenant-scoped credential enrollment and a bounded
  authenticated health check; no product, publication, document, regulated
  write or synchronization capability is admitted;
- the host-mediated catalog probe rejects non-HTTPS/private/unknown hosts,
  unknown credential placeholders and unconfigured endpoints, and normalizes
  provider failures without exposing secrets;
- API account enablement, generated Go/TypeScript catalogs, frontend category
  presentation and public documentation use the same contract;
- focused and full Go tests/vet, contract generation, frontend UI tests and
  production frontend build: **PASS**;
- domain workflows remain qualification-gated and are not claimed as live
  production integrations without official non-production evidence.

## Connector category layout validation

- all 58 built-in providers now live under one family-derived category level:
  `connectors/<category>/<provider>`;
- architecture policy, provider review evidence, runtime imports, lifecycle
  inventory and generated frontend/Go catalogs use the categorized paths;
- category-family mismatches fail closed in the architecture checker and
  frontend catalog generator;
- focused Go tests, all connector tests, `go vet`, contract validation,
  frontend logic tests (30/30) and clean production Go/frontend Docker builds:
  **PASS**;
- no runtime API or database contract changes were introduced by the move.

## Task 155 repository validation

- «Почта России» is visible as a separate logistics card in «Интеграции →
  Доставка»;
- the official Otpravka application token and user authorization key remain
  callback-scoped and the host performs only the fixed `/1.0/settings` probe;
- exact `Authorization: AccessToken` and `X-User-Authorization: Basic`
  headers, strict credential decoding, bounded JSON responses and fixed-host
  egress are covered by deterministic tests;
- rates, shipments, labels, returns, pickup points and tracking remain
  fail-closed pending a current non-production carrier qualification;
- generated catalog/runtime support, policy/review, connector docs and
  conformance evidence are synchronized (**56 manifests / 18 generic / 38
  separate / 0 planned**).

## Task 153 repository validation

- CS-Cart is visible as a separate `storefront` card in «Интернет-магазины»;
- the official CS-Cart API 2.0 Basic Auth adapter admits bounded product
  catalog reads, creates/updates and inbound/outbound product sync;
- administrator e-mail/API key stay callback-scoped and store host settings
  are non-secret runtime configuration;
- inventory, prices, orders and webhooks remain fail-closed because no matching
  worker routes are claimed;
- focused connector and builtin-runtime tests: **PASS**;
- live qualification is **not claimed** without a non-production CS-Cart store
  with API access enabled. The credentialed check is now provided by
  `scripts/cscart-smoke.sh` and is tracked separately in
  `docs/connectors/cs-cart/live-qualification-status.json` (**BLOCKED** until
  such a store and key are configured);
- CS-Cart generated catalog/runtime rows are synchronized (**56 admitted
  manifests / 18 generic / 24 separate / 14 planned**); Saleor and «Почта
  России» are registered by Tasks 154–155.

## Task 152 repository validation

- 1С-Битрикс is visible as a separate `storefront` card from Bitrix24 CRM;
- the official REST-module webhook bridge admits bounded product catalog
  reads, idempotent product writes and inbound/outbound product sync;
- webhook credentials remain encrypted and `catalog_iblock_id` is required
  non-secret runtime configuration;
- inventory, prices, orders, offers/custom properties and webhook receipt stay
  fail-closed because no matching worker routes are claimed;
- focused connector and builtin-runtime tests: **PASS**;
- live qualification is **not claimed** without a dedicated self-hosted
  1С-Битрикс site, enabled REST module and non-production webhook.
- generated catalog/runtime support: **PASS — 53 manifests / 16 generic / 23
  separate / 14 planned**.

## Tasks 149–150 repository validation

- Ollama, LM Studio and Open WebUI are visible on the dedicated AI-provider
  surface and selectable in Reports → Ask AI;
- all three use the governed `ai.completion.generate` route and a typed
  OpenAI-compatible `/chat/completions` adapter; no product synchronization,
  streaming or tool capability is advertised;
- local egress is host-mediated and allowlisted to private/loopback Docker
  endpoints with pinned dialing, proxy/redirect suppression and bounded bodies;
- migration `000021`, OpenAPI, runtime support, generated catalogs and
  architecture reviews are synchronized;
- generated catalog: **PASS — 52 manifests / 14 generic / 23 separate / 15
  planned**;
- live model availability is **not claimed** without operator-provided Ollama,
  LM Studio or Open WebUI services and a non-production model/account.

## Task 147 repository validation

- Ozon Pay is visible on the separate Payments surface with encrypted
  `client_id`/`api_key` enrollment and a bounded Seller API access probe;
- Ozon Доставка is visible on the separate Delivery surface with encrypted
  Seller API enrollment and a bounded warehouse probe;
- payment mutations and delivery rates/shipments/labels/tracking remain
  fail-closed until the current Ozon merchant contracts are qualified;
- generated catalog/runtime support: **PASS — 48 manifests / 13 generic / 20
  separate / 15 planned**;
- full `go test ./...` and `go vet ./...` in the Go 1.26 validation image:
  **PASS**;
- focused Ozon transport and connector tests: **PASS** (full repository checks
  are listed below);
- Compose `frontend` and `api` images rebuilt and restarted; both containers
  are healthy, API `/api/v1/health` returns `status: ok`, and the served
  frontend bundle contains `ozon-pay` and `ozon-delivery`.

The standalone architecture checker retains only pre-existing dirty-tree
findings (an unregistered Shopware directory and earlier provider conformance
notes); no Ozon-specific finding is introduced.

## Task 145 repository validation

- CDEK is no longer `planned`: it is shown as «СДЭК» on the separate Delivery
  surface with an OAuth client-credentials token probe and bounded city read;
- Деловые Линии is present on the same surface with encrypted appkey/PAT
  enrollment and an authenticated v4 login probe;
- `./scripts/check-contracts.sh`, `go test ./...` and `go vet ./...` in the
  pinned Go 1.26 image: **PASS**;
- generated catalog/runtime support: **PASS — 45 manifests / 12 generic / 18
  separate / 15 planned**;
- frontend logic tests and production Vite build: **PASS**;
- live carrier qualification is **not claimed** without tenant credentials and
  retained provider evidence; shipment/rate/label/return routes remain closed.

The standalone architecture checker still reports pre-existing findings in
other dirty-tree work (Robokassa/Shopify payment branches and an unregistered
Medusa directory); those findings are outside Task 145 and are not hidden by
this report.

## Inventory

- repository-implemented tasks: `001`–`157`; live provider qualification for
  Bitrix24, 1С-Битрикс and logistics carriers remains an external environment gate;
- architecture policy: **127 modules / 58 provider modules / 147 reviews**;
- active PostgreSQL baseline: **21 migrations**, latest `000021`; archived pre-v1 lineage: **74 migrations**, legacy head `000074`;
- public OpenAPI/generated SDK surface: **138 operations / OpenAPI 0.21.1**;
- connector catalog: **58 manifests / 18 generic runtime integrations / 40
  providers on separate surfaces / 0 planned**;

## Task 141 repository validation

- Claude is admitted as an AI provider through the existing
  `/settings/ai-providers:analyze` operation; provider dispatch remains confined
  to the built-in registry and no generic product synchronization is claimed;
- the connector sends Anthropic Messages API requests through the common
  host-owned HTTPS transport, with `x-api-key` and `anthropic-version` headers;
- migration `000020` widens the tenant AI-provider allow-list without changing
  existing credential rows;
- generated runtime support/catalog: **PASS — 40 manifests / 11 generic / 14
  separate / 15 planned**;
- focused Claude connector, runtime support and migration tests: **PASS**;
- live Claude health is **not claimed** without a tenant API key and a
  non-production account.
- repository license: **Apache-2.0**.

## Task 139 repository validation

- Bitrix24 is admitted as a dedicated `crm` separate surface through the
  built-in registry; generic product synchronization remains fail-closed;
- the common host-owned transport sends only `Authorization: Bearer` and the
  non-secret lower-case `portal_host` is validated before egress;
- generated runtime support/catalog: **PASS — 39 manifests / 11 generic / 13
  separate / 15 planned**;
- frontend UI logic and package-index generation: **PASS**;
- live Bitrix24 OAuth/portal health is **not claimed** without a dedicated
  non-production Bitrix24 tenant and OAuth application.

## Task 134 repository validation

- `go test ./...` and `go vet ./...` in the pinned Go 1.26.7 validation image: **PASS**;
- focused OAuth manager/runtime/API/worker/Bitrix24 tests: **PASS**, including 12 concurrent expired-token consumers producing exactly one refresh and one immutable rotation;
- rejected refresh material maps to reauthorization, while temporary endpoint failures remain separately bounded: **PASS**;
- contracts and task numbering: **PASS — 138 operations / Tasks 001–138 contiguous**; Task 134 itself changes no public API;
- architecture: **PASS — 125 modules / 38 providers / 125 reviews**;
- migration catalog and pre-v1 equivalence: **PASS — 19 active / latest 000019**; Task 134 adds no migration;
- generated runtime catalog: **PASS — exact 38-manifest parity; 11 generic / 9 separate / 18 planned**;
- frontend logic, typecheck, static policy and production Vite build: **PASS**;
- Community deployment policy: **PASS**;
- deployed artifacts: **PASS — API healthy with Task-134 health codes, worker restarted with all eight components, frontend restarted healthy with «Войти снова», `/integrations` returns 200 and unauthenticated `/api/v1/connector-accounts` returns 401**.

Task 134 removes the repeated-login defect at the generic connector boundary:
authorization-code credentials refresh before expiry, concurrent API/worker
callers serialize refresh-token use in PostgreSQL, and provider adapters receive
only a callback-scoped access token. A rejected/revoked refresh token still
requires an explicit operator login, which is shown as «Войти снова». Live
provider refresh is not claimed without a non-production OAuth account and
retained remote evidence.

## Task 133 repository validation

- `go test ./...` and `go vet ./...` in the pinned Go 1.26.7 validation image: **PASS**;
- MAX connector, built-in transport/registry, Social API and worker focused tests: **PASS**;
- contracts and task numbering: **PASS — 134 operations / Tasks 001–133 contiguous**;
- architecture: **PASS — 125 modules / 38 providers / 124 reviews**;
- migration catalog and pre-v1 equivalence: **PASS — 17 active / latest 000017; no new migration**;
- generated runtime catalog: **PASS — exact 38-manifest parity; 11 generic / 9 separate / 18 planned**;
- frontend logic: **PASS — 30/30 tests**; repository shell/static-policy gate and production Vite build: **PASS**;
- Community deployment policy: **PASS**;
- rebuilt API, worker and frontend: **PASS — API/frontend healthy, Social worker component running, `/social` returns 200 and unauthenticated Social API returns 401**.

Task 133 moves MAX from planned to a working separate Social surface and admits
only `social.post.text` with a 4000-code-point ceiling. Live MAX delivery is
**not claimed**: this environment contains no MAX connector account, bot token
or dedicated test channel. The final RUNTIME-133 provider gate requires those
non-production credentials and retained remote-delivery evidence.

## Task 132 repository validation

- `go test ./...` and `go vet ./...` in the pinned Go 1.26.7 validation image: **PASS**;
- contracts and task numbering: **PASS — 134 operations / Tasks 001–132 contiguous**;
- generated Go/Python/TypeScript SDK drift and runtime tests: **PASS**;
- architecture: **PASS — 125 modules / 38 providers / 123 reviews**;
- frontend logic/static-policy checks and production Vite build: **PASS**;
- migration catalog/static baseline: **PASS — 17 active / latest 000017**;
- live local migration: **PASS — 17/17**, with a pre-migration PostgreSQL backup checkpoint;
- append-only application-role privileges: **PASS — SELECT/INSERT allowed; UPDATE/DELETE/TRUNCATE denied**;
- rebuilt API, worker and frontend: **PASS — API/frontend healthy; Social worker component running; `/social` returns 200 and unauthenticated Social API returns 401**.

Task 132 moves Telegram from planned to a working separate Social surface and
admits only `social.post.text`. Live Telegram delivery is **not claimed**: this
environment contains no Telegram connector account or bot token. The final
RUNTIME-132 provider gate requires a non-production bot and dedicated test
channel, as defined by the connector conformance plan.

## Task 131 validation

- `go test ./...` and `go vet ./...` in the pinned Go 1.26.7 validation image: **PASS**;
- runtime-support generation and exact 38-manifest parity: **PASS**;
- contracts, JSON Schema fixtures, architecture and package index: **PASS**;
- frontend logic tests and production Vite build: **PASS**;
- Community deployment policy, including configurable bridge MTU: **PASS**;
- opt-in live transport probe against the official dated Bank of Russia XML
  endpoint: **PASS**;
- rebuilt worker live refresh: **PASS — 53/53 reviewed currency pairs**;
- PostgreSQL runtime evidence: **PASS — 53 distinct base currencies persisted
  with `source_id = cbr`**;
- API, frontend and infrastructure health: **PASS**.

Task 131 moves only CBR FX from planned to working. It does not claim runtime
readiness for the remaining 20 connectors. IRR/RUB is explicitly excluded:
the source's million-unit nominal exceeds the platform's exact decimal scale,
and financial observations are never silently rounded.

## Task 130 validation

- `go test ./...` in the digest-pinned Go 1.26.7 build image: **PASS**;
- `go vet ./...`: **PASS**;
- runtime support generation: **PASS — exact parity with all 38 manifests**;
- ready connector registry resolution: **PASS — 11 product readers; outbound product sync limited to OpenCart and WooCommerce**;
- OpenAPI/contracts and JSON Schema fixtures: **PASS — 129 operations / 0.21.1**;
- generated Go/Python/TypeScript SDK drift and runtime tests: **PASS**;
- frontend logic tests, TypeScript checks, static policy and production Vite build: **PASS**;
- architecture: **PASS — 124 modules / 38 providers / 121 reviews**;
- API, worker and frontend images rebuilt and containers recreated: **PASS**;
- live Community state: **API healthy, frontend healthy, worker reconciliation runtime ready; `/api/v1/health` returns `ok`**.

Task 130 does not claim that all 38 SDK connectors are end-to-end production
integrations. The product now presents that distinction explicitly and rejects
manifest-only account/capability/synchronization operations before dispatch.

## Task 129 validation

- `go test ./...` with `git` available in the digest-pinned Go 1.26.7 image: **PASS**;
- `go vet ./...`: **PASS**;
- `govulncheck -test ./...`: **PASS — no reachable vulnerabilities**;
- OpenAPI/contracts/runtime parity: **PASS — 129 operations / 0.21.0**;
- generated Go/Python/TypeScript SDK drift and runtime tests: **PASS**;
- frontend typechecks, 24 tests, static policy and production Vite build: **PASS**;
- migration inventory/baseline: **PASS — 16 active / latest 000016**;
- isolated PostgreSQL 18 migration/application-role/RLS/append-only smoke: **PASS**;
- architecture: **PASS — 124 modules / 38 providers / 120 reviews**;
- Community deployment and JavaScript supply-chain policy: **PASS**;
- aggregate repository supply-chain policy: **PASS — current CI, Go/JS/Python module, image and command inventories are covered fail-closed**;
- scratch runtime Docker build from the pinned Go image: **PASS — five binaries**;
- existing `.env` upgrade path: **PASS — preserves current values and adds one 64-hex application-role password idempotently**;
- live Community upgrade: **PASS — migration 16/16; API/MCP/frontend healthy; scheduler/worker running; seven runtime sessions use `torgnexa_app`; seven trust tables have FORCE RLS**;
- upload-security ZIP64 overflow regression and targeted scan: **PASS**; the remaining ClamAV chunk-size cast warning is pre-existing and bounded by the fixed 64 KiB buffer.

`scripts/check-supply-chain.sh` now validates the current repository topology rather than a stale smaller inventory. It requires exactly the constrained `go` and `javascript` CI jobs, recognizes six exact Go modules (including one exact local SDK example replacement), accepts only registered npm/Python package files, safely parses bounded Compose anchors while retaining strict contract YAML parsing, admits the digest-pinned Node build image, and accounts for every `cmd/*` as either a release binary or an explicit source-only command. Unknown modules, ecosystems, jobs, images, replacements and commands remain denied.

The sections below retain historical Task 118–121 evidence. Their old host/tool availability notes are superseded by the Task 129–130 containerized checks above.


## P4 repository checks

- P4 fail-closed policy tests: **PASS — 5 tests**;
- P4 Python source AST/syntax checks and shell `bash -n`: **PASS**;
- production posture positive fixture: **PASS**;
- deterministic release-evidence packaging: **PASS — identical SHA-256 across repeated builds**;
- release/required-workflow YAML parse and P4 publication invariants: **PASS**;
- P3→P4 architecture diff: **PASS — 33 changed files, ARCH-118 exact scope, `git diff --check` clean**;
- static architecture: **PASS — 117 modules / 32 providers / 109 reviews**;
- migration catalog/baseline: **PASS — 11 active migrations / latest 000011; 74 archived legacy migrations verified**;
- generated SDKs: **PASS — 108 operations / OpenAPI 0.15.0**;
- frontend: **PASS — 18/18 tests, 32-connector catalog, static policy**;
- JS supply-chain repository/lock policy: **PASS**;
- Community deployment policy: **PASS**;
- public API remains 108 operations / OpenAPI 0.15.0; Task 121 changes migration packaging/history lineage only and introduces no business schema change.

P4 external PASS is intentionally unavailable on this host because it requires a real exact release tag, Docker/Go 1.26.5, GitHub applied-rules and draft-release APIs, protected OIDC evidence and live connector credentials. The repository now fails closed on all of those missing inputs; a local `make p4-qualification` on this host stops immediately because Docker is unavailable.


## Task 121 migration-baseline checks

- active `migrations/*.sql`: **11 files**;
- archived `migrations_legacy_pre_v1/*.sql`: **74 files**;
- every archived SQL SHA-256 matches the archived original catalog;
- `scripts/generate-pre-v1-baseline.py --check`: **PASS**;
- `scripts/check-pre-v1-baseline.sh`: **PASS**;
- active migration catalog checker: **PASS — 11 entries**;
- Task 120 → Task 121 architecture diff: **PASS — 159 changes / exact ARCH-121 scope**;
- current static architecture: **PASS — 117 modules / 32 providers / 112 reviews**;
- active and legacy deploy TSV parity / legacy catalog digest: **PASS**;
- one-time rebaseline path is explicit/fail-closed and preserves all legacy history rows before stamping the compact baseline.

## Task 120 Enterprise Operations UX checks

- frontend experience suite: **PASS — 23/23**;
- generated SDK gate: **PASS — 108 operations / OpenAPI 0.15.0**;
- Product/Order list UX: server-owned `q`/status/cursor pagination, no whole-tenant browser materialization;
- realtime: protected metadata-only SSE + authenticated fetch/reconnect + query-cache invalidation;
- Incident Center: warehouse/drift/connector/approval aggregation with deep routes;
- global command search: product/order queries executed on their existing server search endpoints;
- analytics: replay-safe report projection for Dashboard KPIs and accessible SVG 7/30/90-day charts;
- active migrations are **11 / latest 000011**; the previous 74-file chain remains immutable archive evidence at legacy head 000074.

The focused Go realtime handler test is present, but this host cannot execute the canonical API package on the pinned Go 1.26.5 toolchain because only Go 1.23.2 is installed and required external modules/toolchain downloads are unavailable. It is therefore not reported as a runtime PASS here.

## Task 119 UI checks

- operator dashboard/onboarding source regression — PASS;
- labelled SVG navigation, mobile labels, `aria-current`, focus-visible and reduced-motion regression — PASS;
- DataTable search/sort/pagination/selection/column/bookmarkable-view regression — PASS;
- warehouse incident and fulfillment-lineage UI regression — PASS;
- integration overview + focused drawer regression — PASS;
- Command Palette / activity center / dark-theme source regression — PASS;
- `tsc -p frontend/tsconfig.repository.json` — PASS;
- `./scripts/check-frontend-shell.sh` — PASS, 18 tests / 32 connectors / static browser-state policy.

Task 119 adds no migration, public API operation, event or generated SDK change. Task 120 adds one additive realtime API operation and no migration/event change.

## P3 repository checks completed here

- targeted `internal/core/inventory` tests — PASS;
- targeted `internal/platform/postgres/inventoryrepo` tests — PASS;
- `./scripts/check-migrations.sh` under the locally compatible checker declaration — PASS, 11 active baseline migrations / latest 000011 plus deterministic legacy-head equivalence;
- `./scripts/check-architecture.sh` under the locally compatible checker declaration — PASS, 117 modules / 32 providers / 109 reviews;
- fulfillment event Draft 2020-12 schema valid fixture — PASS;
- fulfillment event invalid fixture rejection — PASS;
- event catalog/fixture strict ordering and uniqueness — PASS;
- SDK regeneration + `./scripts/check-generated-sdks.sh` — PASS, 108 operations / OpenAPI 0.15.0;
- `.github/workflows/release.yml` YAML parse — PASS;
- `bash -n scripts/check-production-qualification.sh` — PASS;
- `bash -n scripts/check-p3-release-qualification.sh` — PASS.

The stock/reservation execution code was also recompiled through its normal inventory repository package with the root module directive temporarily lowered only for the local Go 1.23 checker; the shipping `go.mod` remains Go 1.26.0 / toolchain 1.26.5.

## P3 warehouse invariants

Migration 000074 and the runtime implementation enforce:

- at most one active `reserved` fulfillment allocation per order item;
- allocation offer/quantity/unit exactly match the immutable order item;
- warehouse identity cannot be rewritten on an allocation;
- a failover creates a replacement allocation and releases the source allocation;
- source/destination reservation changes are atomic and quantity-conserving;
- destination ATP is locked/rechecked before commit;
- physical `on_hand` is not modified by failover;
- terminal orders cannot receive replacement allocations;
- untracked/inconsistent reservations are not guessed;
- tenant FORCE RLS remains active;
- allocation and position changes emit transactional Outbox evidence.

## Release DAG qualification

The release workflow now has an explicit `runtime-qualification` job and `build.needs` includes that job. Therefore a normal protected release workflow cannot proceed to build/sign/publish unless the P3 deployed-image qualifier succeeds.

The qualifier now requires the synthetic order allocation to be rerouted to the configured backup and verifies all of:

- incident terminal state;
- at least one routed offer;
- at least one rerouted allocation;
- zero execution-attention findings in the positive test;
- source on-hand unchanged;
- source reservation returned to its pre-test baseline;
- destination reserved increased;
- source allocation released;
- exactly one replacement allocation tied to the incident;
- fulfillment allocation Outbox events observed.

The same probe is repeated after worker, Kafka and PostgreSQL restart drills.

## License gate

The repository license decision is resolved as Apache-2.0. `LICENSE`, `LICENSE-DECISION.md`, owned package metadata and `supply-chain/release-artifacts.json` are aligned. This does **not** override package/dependency license policy or any release security gate.

## Evidence not available on this host

This host does not have Docker and exposes Go 1.23.2 while the repository pins Go 1.26.5. Network access to Go proxy/toolchain downloads is unavailable. Therefore the following are intentionally not reported as PASS here:

1. `go test ./...`, `go vet ./...`, `make check` on Go 1.26.5;
2. `make production-qualification` / `make p3-qualification` on Docker;
3. current two-platform container vulnerability scans;
4. protected GitHub OIDC prerelease/signature/provenance verification;
5. Task 080 hosted Ruleset/Required Workflow/reviewer proof;
6. live seller-account qualification for remote connectors.

Task 118 now composes these external facts into `p4-go-live.json` and provides a separate promotion step. A release runner must retain that evidence; absence cannot be converted into repository-local success.
