# Backlog

The executable backlog is represented by the task cards in `tasks/issues/`. Do not add provider-specific behavior directly to Core.

## Settings delivery sequence

098 account profile and provider-owned password management; 099 organization/workspace settings; 100 members and roles; 101 OIDC/VK identity providers; 102 notification preferences; 103 settings security/audit view. Tasks 098–103 are implemented; Task 101 preserves the identity, tenancy, secrets and audit boundaries declared in its task card.

## Integration settings delivery sequence

104 manifest-driven integration catalog; 105 connector accounts and secret references; 106 authentication/OAuth connection flow (repository-complete); 107 enabled capabilities and sync directions; 108 dry-run/bootstrap/schedules (repository-complete); 109 bounded health history, rate-limit/failure operations and reauthorization visibility.

## Post-baseline schema cleanup

112 retires the empty pre-Task-009 `inbox_events` compatibility placeholder through a backup-gated, fail-closed contract migration; `inbox_receipts` remains the sole runtime consumer inbox.

## Post-baseline runtime composition

113 replaces the placeholder worker with the supervised production composition root for PostgreSQL leases, outbox→Kafka publication, Kafka→webhook delivery, reconciliation execution and optional upload-security scanning. Provider-specific reconciliation source bridges remain fail-closed follow-up work.

## Runtime-truthful integration catalog

130 replaces manifest-implied availability with an exact generated runtime
support contract. Eleven connectors have a generic product bridge, seven AI
connectors belong to their dedicated settings surface, CBR FX runs in Finance,
and 20 entries remain discoverable but fail closed until an end-to-end
application bridge is added. Task 131 closes the first planned connector with
real production composition rather than changing its label alone. Task 156
later replaces the remaining planned labels with explicit health-only category
surfaces; see the current inventory below.

## Bitrix24 CRM runtime closure

Task 139 admits Bitrix24 on a dedicated CRM surface. OAuth/refresh, strict
portal-host configuration, health and the qualified CRM entity/product-row
capabilities use the existing connector-account boundary and built-in registry;
generic product synchronization remains deliberately unavailable. The current
runtime inventory is 11 generic integrations, 13 separate-surface providers
and 15 planned entries.

## Claude AI provider runtime closure

Task 141 admits Claude (Anthropic) on the existing AI-provider settings surface.
The connector uses the host-owned HTTPS boundary and Anthropic Messages API for
one bounded text completion; credentials remain callback-scoped in
SecretProvider, and the generic product-sync surface is unchanged. The current
runtime inventory is 11 generic integrations, 14 separate-surface providers
and 15 planned entries.

## 5Post logistics connector

Task 142 adds a truthful 5Post SDK adapter and admits its account and
credential-check path on the separate Delivery surface. Shipment writes remain
qualification-gated; the current runtime inventory is 12 generic integrations,
16 separate-surface providers and 15 planned entries.

## ПЭК logistics connector

Task 143 adds the ПЭК SDK adapter, official Basic credential probe and
deterministic conformance candidate. ПЭК is available in the same Delivery
surface for tenant account setup, health checks and bounded read-only
`pickup.points.read`, `logistics.rates.read` and `logistics.track.read` routes;
one-code `logistics.shipment.cancel` annuls a previously created
pre-registration through the official order endpoint, `logistics.label.read`
returns one validated PDF label, and `logistics.shipment.create` submits one
bounded self-delivery preregistration with explicit tenant sender settings.
Cancellation of a formed cargo, returns and application print forms remain
closed until a separate non-production API qualification.

## Yandex Market inventory write

Task 172 adds the provider-neutral `inventory.write` runtime path for Yandex
Market. It preserves the provider's distinction between partner-warehouse
`POST` and grouped-warehouse `PUT`, validates warehouse scope and quantity
bounds, and records the provider's asynchronous `OK` as acceptance rather than
reconciliation.

## ПЭК bounded shipment create

Task 173 admits the existing ПЭК `/preregistration/submit/` operation through
the approval-bound logistics shipment route. The runtime accepts only one
Russian self-delivery order (`orderType=0`, `pek_type_3`, up to 50 parcels),
requires tenant-scoped sender warehouse/legal data, verifies `documentId` and
one numeric `cargoCode`, and leaves formed-cargo cancellation, returns and
batch print forms fail-closed.

## Telegram media publication worker route

Task 174 is repository-complete: the existing Telegram photo, 2–10 photo
album and MP4-video adapter is now composed through the Social worker. Core
image/gallery/video variants are mapped to the provider-neutral request and
released uploads are revalidated through the Task-088 gate before each read.
The host transport supports bounded multipart egress. URL buttons, edit/delete,
inbound webhooks and arbitrary file types remain fail-closed; credentialed live
Telegram qualification remains a separate release gate.

## Telegram HTTPS publication buttons

Task 181 is repository-complete: `social.post.buttons` now crosses the
provider-neutral Core variant, tenant-scoped PostgreSQL snapshot, Social API
and leased worker into Telegram's existing URL-only Bot API markup adapter.
The UI exposes up to eight HTTPS link buttons, validates them before submit and
shows them in publication history. Callback-data buttons, edit/delete and
inbound webhooks remain fail-closed because they need separate authorization
and inbound lifecycle contracts. Credentialed live Telegram qualification is
still a release gate.

## MAX media publication worker route

Task 175 is repository-complete: the existing MAX image/gallery and supported
video adapter is now composed through the Social worker. The host admits the
documented `/uploads?type=image|video` flow, restricts upload URLs to the exact
official hosts and sends bounded multipart `data` bodies with the callback-
scoped bot token. Webhooks, status reads, destructive mutations and
arbitrary files remain outside the application runtime subset; credentialed
live MAX qualification remains a separate release gate.

## MAX HTTPS publication buttons

Task 182 is repository-complete: the existing MAX `social.post.buttons` adapter
capability is admitted by the built-in runtime-support contract and generated
catalog. The existing Social API, leased worker, account/channel gates and UI
now expose bounded HTTPS URL buttons for MAX; webhooks, status reads and
destructive mutations remain outside the application runtime subset.

## MAX inbound webhook reception

Task 183 is repository-complete: inbound MAX Webhook updates are now admitted
through the public tenant-bound social webhook route. The route checks the
account and enabled capability, extracts the ephemeral provider secret,
delegates canonical verification to MAX and commits a minimized
`commerce.social.webhook_received.v1` event through the tenant-scoped Inbox and
transactional outbox. Edit/delete and webhook subscription lifecycle calls
remain fail-closed at the application boundary; live MAX credentials and
provider delivery qualification remain release gates.

## Почта России: формирование партии

Task 184 is repository-complete: the qualified `logistics.batches.create`
capability now calls the official `POST /1.0/user/shipment` endpoint through an
approval-bound `POST /api/v1/logistics/batches` route. The request is bounded
to 1–100 numeric backlog order IDs, optional sending date and online-balance
flag; the host persists only a normalized batch result in the tenant-scoped
operation receipt. Replays never issue a second provider request, and an
ambiguous transport outcome stays pending until reconciliation. Postal handoff
and separate return shipment operations remain fail-closed.

## Почта России — передача партии в работу

Task 185 is repository-complete: the qualified `logistics.batches.submit`
capability now calls the official Russian Post check-in endpoint through an
approval-bound `POST /api/v1/logistics/batches/{batch_id}/submit` route. The
adapter sends no body, optionally enables `useOnlineBalance=true`, and accepts
only a response confirming `f103-sent`. The tenant-scoped operation receipt
prevents duplicate handoff and keeps ambiguous outcomes pending until
reconciliation. Separate return shipments remain qualification-gated.

## Почта России — отдельная возвратная отправка

Task 186 is repository-complete: the qualified
`logistics.return.separate.create` capability now calls the official
`PUT /1.0/returns/return-without-direct` endpoint through an approval-bound
`POST /api/v1/logistics/returns/separate` route. The adapter sends exactly one
bounded item, accepts only `position=0` with a valid `return-barcode`, and
stores only the normalized tracking result. Addresses and names stay
request-scoped; the operation receipt prevents duplicate calls and blocks
blind retry after an ambiguous result. Cancellation of already formed batches
remains a separate qualification task.

## Robokassa merchant refund runtime

Task 176 is repository-complete: Robokassa refunds now use the official
merchant Refund API. The runtime obtains `Info.OpKey` from the authenticated
OpStateExt response, signs full/partial refund JWTs with the separately
configured Password3 and stores the asynchronous provider `requestId` as an
accepted refund result. Three-line legacy secrets remain valid for payment,
status, reconciliation and webhooks; the fourth Password3 line is required
only to execute refunds. Live merchant credentials and fiscal receipt
qualification remain deployment-specific gates.

## Почта России — возвратная этикетка

Task 177 is repository-complete: the existing `logistics.label.read` route now
accepts explicit `return_pdf` requests for domestic/S10 RPO barcodes and calls
the one-page easy-return PDF form. The host validates the response as a PDF and
returns only a content-addressed opaque reference. Separate return shipments,
batch formation and hand-off remain qualification-gated.

## ПЭК — печатная форма заявки

Task 178 is repository-complete: the existing `logistics.label.read` route now
accepts explicit `request_pdf` requests and calls the official PEK
`/api/v1/order/print/` endpoint with `type=big`. The bounded base64 response is
validated as a PDF and exposed only as an opaque digest reference; the UI offers
the document type next to the existing single-cargo label (`type=simple`).
Batch printing (`type=multiple`), formed-cargo cancellation, returns and other
write operations remain qualification-gated.

## Почта России — чтение партий

Task 179 is repository-complete: the bounded `logistics.batches.read` route
reads the official `GET /1.0/batch` directory with page, size and optional
mail-type/category filters. The adapter exposes only validated batch identity,
status, shipment count and observation time; order rows and raw provider
payloads stay behind the host boundary. Batch formation and hand-off remain
qualification-gated.

## СБП — admission payment webhook

Task 180 is repository-complete: the existing SBP verifier is admitted through
the shared public payment webhook receiver. It re-fetches the authoritative
status over the account's mTLS gateway, records replay-deduped evidence and
uses the canonical payment transition path. Live acquiring-bank callback
delivery and its current contract remain a separate qualification gate.

## CDEK and Деловые Линии delivery verification

Task 145 admits the existing CDEK SDK on the Delivery surface with an OAuth
client-credentials probe and adds the Деловые Линии adapter with appkey/PAT
session verification. CDEK, ПЭК and Деловые Линии expose bounded read-only rate
previews, and CDEK and Деловые Линии expose bounded tracking reads. Деловые Линии
также допускает ограниченное address-to-address создание отправления с явной
runtime-конфигурацией, а также PDF-форму накладной по UID документа; отмена и
product synchronization remain closed until current
provider fixtures and an idempotent host bridge are qualified. At that point the runtime inventory was
12 generic integrations, 18 separate-surface providers and 15 planned entries.

## Ozon Pay and Ozon Доставка runtime surfaces

Task 147 adds separate finance and Delivery cards for the Ozon services that
are commonly used together by internet shops. Both accept encrypted Seller API
`client_id`/`api_key` credentials and perform a bounded host-mediated health
probe. Payment mutations and delivery rates/shipments/labels/tracking remain
qualification-gated; a healthy Seller API key does not imply merchant-service
activation. The runtime inventory is 13 generic integrations, 20 separate-
surface providers and 15 planned entries.

## Local AI provider runtime

Tasks 149–150 add Ollama, LM Studio and Open WebUI to the dedicated AI-provider
surface. Each uses the existing governed non-streaming completion API and a
host-mediated local transport with private-address pinning, an explicit local
hostname allowlist, no proxy/redirect handling and bounded bodies. The three
cards are configurable from Settings → AI providers and Reports → Ask AI;
model servers remain operator-managed and no generic commerce synchronization
is claimed. The runtime inventory is 14 generic integrations, 23 separate-
surface providers and 15 planned entries.

## 1С-Битрикс storefront runtime

Task 152 adds a separate 1С-Битрикс internet-store card and host-mediated
official REST-module webhook adapter. Product catalog reads and idempotent
product writes, outbound regular prices and inventory documents are executable;
order reads/status writes are also admitted through the explicit status map.
Inventory and price reads, offers and custom properties remain outside the
generic runtime until their inbound bridges are added.

## CS-Cart storefront runtime

Task 153 adds CS-Cart as a self-hosted internet-store card using the official
REST API 2.0 and HTTP Basic Auth (administrator e-mail plus API key). Product
catalog reads, creates and updates are admitted with cursor pagination,
idempotent SKU lookup and read-after-write reconciliation; base price and
inventory reads/writes, plus order reads and standard order-status writes, are
admitted through the product projection and order detail/update endpoints with
inbound/outbound reconciliation; custom status codes and webhooks remain
fail-closed. The runtime inventory is now 17
generic integrations, 23 separate-surface providers and 14 planned entries.

## Saleor storefront runtime

Task 154 adds Saleor as a self-hosted GraphQL storefront connector. Product,
inventory, price and order operations reuse the existing host-mediated SDK
boundary; unsupported mutations remain fail-closed. The runtime inventory is
18 generic integrations, 23 separate-surface providers and 14 planned entries.

## Shopify storefront qualification

Task 144's Shopify Admin REST connector is pinned to the current stable API
version `2026-07`. Since Shopify has no official self-hosted Docker store, the
local stateful protocol double and smoke script qualify request/response shapes,
writes and reconciliation only; a Shopify Dev Store with an installed app,
scoped token and synthetic SKU remains required for external live qualification.
Shopify marks REST legacy for new apps, so a future GraphQL Admin API migration
must be handled as a separately scoped compatibility task rather than hidden in
this qualification.

## Shopware storefront qualification

Task 148's Shopware 6 connector is credential-smoke-tested against a disposable
Shopware 6.7 Docker store using a temporary Integration credential. The gate
covers client-credentials OAuth, JSON:API and flat DAL response mapping,
catalog/detail, EUR price, stock, orders, refunds, product/price/stock writes,
read-after-write reconciliation and automatic cleanup. The Dockware image is a
community-supported disposable fixture; external merchant staging remains
blocked until an HTTPS endpoint, scoped Integration credential and synthetic SKU
are supplied. See `docs/connectors/shopware/docker-live-qualification.md` and
`live-qualification-status.json`.

## Почта России logistics connector

Task 155 adds «Почта России» to the separate Delivery surface with encrypted
application-token/user-key enrollment and a bounded
`otpravka-api.pochta.ru/1.0/settings` probe. Rates, pickup points, a PDF order
form read, a single-barcode tracking read and one strict backlog-order create
are available; batch formation, hand-off, cancellation and returns remain
qualification-gated until current non-production carrier fixtures are
available.

## Connector package layout

Built-in providers are organized under one family-derived category level:
`connectors/<category>/<provider>`. Architecture policy/reviews, lifecycle
inventory and catalog generators enforce this layout while retaining stable
provider IDs and the `docs/connectors/<provider>` documentation paths.

## Workflow automation builder

Task 163 is the in-progress provider-neutral automation builder. Its foundation
and runtime are implemented; it is decomposed
into ten bounded subtasks: ADR/action catalog; canonical immutable workflow
versions; schema-backed DSL/compiler; tenant-scoped PostgreSQL state; EventBus
and durable schedule triggers; execution/retry/approval state machine; typed
safe action adapters; REST/OpenAPI and operator UI; quotas/observability/recovery;
and load/chaos/Compose qualification. The first vertical slice is deliberately
limited to notification, reconciliation, approval and dry-run actions. It
must reuse EventBus/Outbox/Inbox, Task-017 approval, existing connector ports
and the PostgreSQL scheduler. Arbitrary code/SQL/HTTP, provider branches,
unbounded loops and secret/payload persistence are explicitly excluded. See
`tasks/issues/163-workflow-automation-builder.md`.

## Возвраты, отмены и refunds

Task 164 is the planned provider-neutral returns/cancellations/refunds contour.
It extends the existing `payments.Refund` and refund API instead of creating a
second payment state machine, and connects order cancellation, partial line
returns, shipment/carrier operations, receipt/inspection, WMS ledger,
fiscalization, settlement and payment reconciliation. The decomposition has
twelve bounded subtasks: ADR/policy matrix; canonical state machines and
invariants; cross-domain orchestration; PostgreSQL/RLS/evidence; events,
Outbox/Inbox and verified webhooks; cancellation worker; return logistics and
WMS disposition; refund/fiscal/reconciliation runtime; REST/OpenAPI/UI;
connector qualification; security/observability/quotas/recovery; and tests,
Compose, load/chaos and documentation. Unknown external outcomes, duplicate
delivery and crash-after-remote-acceptance are first-class cases; blind retry,
silent ledger rewrites and unqualified connector capabilities are forbidden.
See `tasks/issues/164-returns-cancellations-refunds.md`.

## Прогноз остатков и автопополнение

Task 165 is the in-progress extension of Task 053 from a basic advisory formula to
an explainable forecast and guarded replenishment runtime. It adds forecast
horizons/intervals, data-quality gates, projected stockout/overstock risk,
supplier/MOQ/case-pack and budget/capacity policies, plus three explicit modes:
`recommendation_only` (default), idempotent `draft_po` and narrowly qualified
`auto_submit`. The decomposition has thirteen subtasks covering ADR and policy,
domain contracts, input normalization, deterministic forecast baselines,
projection/scenarios, reorder optimization, PostgreSQL/RLS/lineage, scheduled
worker, procurement execution, REST/UI/MCP boundaries, connector qualification,
security/observability/quotas and Compose/load/chaos qualification. Forecasts
never become inventory truth; PO submission never bypasses the existing
procurement lifecycle or approval, and stale/ambiguous/unqualified inputs fail
closed. See `tasks/issues/165-stock-forecast-auto-replenishment.md`.

## Центр качества публикации товаров

Task 166 is the repository-complete provider-neutral publication preflight and quality
center. It adds target-specific readiness for Product/Offer against each
connector account, deterministic score and rule evidence, hard blockers and
warnings, compliance/capability/freshness checks, remediation and post-publish
drift. The decomposition has thirteen subtasks: ADR/governance; immutable
quality model and snapshots; versioned declarative connector profiles; catalog/
PIM/price/stock/media/compliance snapshot assembly; rule engine and score;
pre-publication gate in `commerce-sync`; event/scheduler worker; PostgreSQL/RLS/
lineage; REST/OpenAPI/UI; safe remediation and approval; connector qualification;
security/observability/quotas; and tests, Compose, screenshots and docs. Task
082's compliance guard remains mandatory, quality never edits Product truth, and
unsupported/stale/unknown profiles fail closed. See
`tasks/issues/166-product-publication-quality-center.md`.

The completed repository slice includes the deterministic quality engine,
declarative profile schema, tenant-scoped PostgreSQL evidence, typed
remediation proposals, read-only operator API, operator UI and a fail-closed
commerce-sync receipt gate. Connector live credentials, Docker/live evidence
and runtime promotion remain release-topology gates and cannot be inferred from
SDK or catalog presence.

## Юнит-экономика по каналам

Task 167 is repository-complete: the provider-neutral factual channel unit-economics
contour. It extends the current `profitability-v1` what-if scenario and the
three basic reports with reproducible actuals by channel, store, order and
Offer/SKU. The decomposition has eighteen subtasks covering accounting
definitions and bases, channel identity/attribution, exact metric contracts,
source fact normalization, historical COGS, deterministic allocation,
settlement/payment deduplication, advertising/promotions, returns/refunds,
FX, immutable calculation runs, ClickHouse projections, PostgreSQL/RLS/
lineage/retention, durable workers, REST/OpenAPI/exports, the operator UI,
security/observability/quotas and full Compose/test/documentation evidence.
The ledger and canonical commerce facts remain authoritative; payout is not
revenue, missing facts are never zero-filled, and mixed currencies require
Task-089b conversion evidence. Settlement corrections remain append-only and
AI/MCP/n8n cannot change financial facts. See
`tasks/issues/167-channel-unit-economics.md`.

## Единый центр состояния интеграций

Task 168 is complete: the provider-neutral integration state center
now composes
account lifecycle, credential/configuration class, truthful runtime stage,
capability grants, health/freshness/rate limits, OAuth reauthorization,
sync/retry/DLQ, reconciliation drift, webhooks, notifications and separate
AI/Finance/Delivery/CRM surfaces into one permission-aware read model. The
decomposition has eighteen subtasks covering status vocabulary and reducer,
bulk source adapters, derived snapshots/RLS/lineage/retention, canonical
events/worker/realtime invalidation, REST/OpenAPI, responsive UI, safe
idempotent operator actions, security/SLO/quotas and complete Compose/test/
documentation evidence. The center performs no remote probe on GET, never
stores secrets and cannot authorize an operation; manifest/health-only/SDK-only
evidence remains fail-closed. See
`tasks/issues/168-integration-state-center.md`.

## AI-помощник для оператора

Task 169 is complete as a provider-neutral operator copilot over the canonical
catalog, publication-quality, inventory/forecast, orders/returns, integration
state, notifications, reports, unit-economics and workflow surfaces. The
current Reports → Ask AI flow is only a caller-assembled completion request;
Task 169 adds server-side intent/retrieval, source evidence and freshness,
grounded answers/refusals, durable runs, typed action previews and the existing
approval/domain execution boundaries. The decomposition has twenty bounded
subtasks covering ADR/threat model, contracts, privacy/retention, model/egress
policy, intent routing, source adapters, citations, prompt-injection boundary,
answer quality, action catalog, approval hand-off, RLS persistence, durable
worker, events/audit/notifications, REST/OpenAPI/SDK, Russian operator UI,
MCP/OpenClaw/n8n boundary, operations, deterministic demo fixtures and full
Compose/test/documentation qualification. The first release is
recommendation/preview-first: no autonomous writes, raw prompt/response,
chain-of-thought, secrets or second source of truth. See
`tasks/issues/169-ai-operator-assistant.md`.

## Маркировка, агрегация и УПД

Task 171 is repository-complete as a provider-neutral marking execution
contour. It adds safe code fingerprints and expiring artifact references,
typed Connector SDK operations for code ordering/reservation, aggregation,
circulation and transfer, package-tree validation, WMS scan outcomes, a
versioned UPD 5.03/EDO state model, reconciliation drift vocabulary and the
operator marking surface. PostgreSQL migration 000037 uses FORCE RLS and
append-only evidence for scans, remote observations and drifts. Chestny ZNAK,
Diadoc, Saby EDO, KKT/OFD and marketplace order/supply writes remain separate
qualification gates and are not implied by manifest or synthetic conformance.
See `tasks/issues/171-marking-execution-and-upd.md` and
`adr/0122-marking-execution-and-edo.md`.

## P0 — Foundation
001-010, 017, 021, 024-025, 060, 063-067.

## P1 — Core commerce and reference integrations
004-016, 018-020, 023, 028-032, 049, 058-059.

## P2 — Channel and operations expansion
033-057, 062, 073-076.

## P3 — Regulated and ecosystem expansion
068-072, 077-085, 087-092.

## P2/P3 — Cloud commercial lifecycle
086 after entitlements/payments/settlement foundations.

Priority may move, but dependency gates in milestones and contracts must be respected.

## Required follow-up discovered during Task 024

- Add a deterministic merge-base compatibility comparison for published OpenAPI, protobuf, webhook, and event versions. The Task 024 gate validates the current tree and version structure but cannot by itself prove that an already-published artifact was not changed incompatibly in the same pull request.

## Required follow-up discovered during Task 065

- Before any release qualification, upgrade or remediate the pinned Kafka and
  PostgreSQL development-runtime images, then repeat both-platform scans against
  a fresh vulnerability database. The 2026-08-09 snapshot found 10 High Kafka
  findings and 15 High plus 1 Critical PostgreSQL finding per platform. A
  2026-08-18 re-scan (`qualification/evidence/supply-chain-scan-2026-08-18/`,
  trivy v0.70.0 against the exact pinned digests) found the PostgreSQL image
  had regressed to 22 High plus 1 Critical (new Go stdlib CVEs since the last
  snapshot); Kafka was unchanged at 10 High. A 2026-08-19 re-check
  (`qualification/evidence/supply-chain-scan-2026-08-19/`) found and applied
  the one fix available: `postgres:18-alpine` had a newer upstream rebuild
  that closes `CVE-2026-33630` (22 High → 21 High); `docker-compose.yml` and
  `supply-chain/release-artifacts.json` are now pinned to that digest. The
  remaining 1 Critical / 21 High on PostgreSQL is a Go-stdlib-v1.24.6 helper
  binary bundled by the upstream image (fix requires an upstream rebuild with
  Go ≥1.24.13, not fixable by any digest/tag choice available today); Kafka's
  10 High is unchanged because `apache/kafka:4.3.1` is still upstream's
  newest non-RC tag. Both remain genuinely blocked on upstream rebuilds, not
  on anything in this repository; re-run the scan periodically to catch the
  next rebuild of either image.
- Obtain legal review of the package-level license inventory for every pinned
  runtime image. Do not infer approval from an image's top-level license and do
  not relax the default-deny SPDX policy to make the gate green. The
  2026-08-18 re-scan confirms both images fail the default-deny SPDX policy
  (`supply-chain/license-policy.json`) on Alpine base-layer packages —
  `GPL-2.0-only` in `postgres:18-alpine` (busybox, apk-tools,
  alpine-baselayout, ...) and the same plus `LGPL-2.1-or-later` in
  `apache/kafka:4.3.1` (gnutls, libgcrypt, acl-libs, ...). Every flagged
  component is an unmodified base OS package, not application code the images
  link against — legal counsel still needs to make the aggregation-vs.-
  derivative-work call before this can be closed; see the evidence README for
  the full per-package inventory.
- Run a protected semantic-version prerelease on the hosting platform and
  independently verify downloaded signatures, provenance, subjects, OIDC
  identity, and archived evidence. Keep publication disabled until protected prerelease, image/security, backup/upgrade and runtime qualification evidence all pass for the exact release topology. The repository license is now Apache-2.0.

## Required follow-up discovered during Task 027

- Before a deployment release, execute and retain an isolated PostgreSQL restore
  against that deployment's real encrypted immutable backup store, KMS,
  credentials, continuous WAL archive, and topology. The synthetic tmpfs drill
  cannot qualify those external controls.
- When Kafka, ClickHouse, object storage, the identity provider, and KMS/HSM
  integrations are selected and implemented, add provider-specific backup and
  restore automation plus synthetic failure drills. Their current recovery
  ownership/order is documented, but placeholder exports or configuration
  checks must not be counted as restored evidence.

## Required follow-up discovered during Task 067

- Every release must rehearse each supported deployed source version against
  the target environment's PostgreSQL extensions/topology, representative
  synthetic or anonymized data, mixed-version application fleet, capacity
  limits, and a Task 027 verified backup checkpoint. The repository's isolated
  PostgreSQL rehearsal cannot qualify those environment-specific controls.
- Wire the driver-neutral backfill repository into the durable scheduler only
  after Task 009 establishes the production background-job/inbox runtime. Do
  not add an ad-hoc database driver or execute externally visible effects
  outside the outbox/inbox/idempotency boundary in the interim.

## Required follow-up discovered during Task 080

- Configure a repository or organization Ruleset Required Workflow (or an
  equivalent immutable external architecture check), protected branch policy,
  and required architecture reviewer. Retain a post-merge pull-request run
  whose base already contains Task 080 and independently confirm that changing
  or deleting the pull-request workflow cannot satisfy the required check by
  reusing its job name. Repository-local tests cannot qualify this hosted trust
  boundary.
- Before provider admission is enabled, reconcile the canonical Connector SDK
  family/capability vocabulary with the capability registry and manifest
  contract. FX and notification/SMS capabilities are planned outside the
  current nine-family connector contract; Tasks 010, 064, 089, and 091 must
  resolve that drift without provider-specific branching in Core.

## Task 114 — P0 runtime closure

Repository-complete: six priority connector reconciliation bridges, production ActionExecutor, Kafka Inbox/idempotency wrapping, versioned non-secret connector runtime config, regenerated public SDKs, and repaired frontend shell typecheck gate.


## Task 115 — P1 operations closure

Repository-complete: connector health history, production notification adapters, resumable privacy execution, and persistent warehouse operational state/failover safety.


## Task 116 — P2 production qualification closure

Repository-complete: durable warehouse incident automation, deployment-image Outbox→Kafka→Inbox qualification with duplicate-idempotency proof, worker/Kafka/PostgreSQL restart drills, bounded API SLO load probe, and a qualified Yandex Market `prices.write` capability. Deployment qualification remains fail-closed until `make production-qualification` passes on the exact release topology and its evidence is retained.


## Task 117 — P3 warehouse execution and release closure

Repository-complete: durable fulfillment allocations, transactional warehouse reservation reroute with immutable replacement lineage, fulfillment Outbox events, upgraded deployed-image qualification, mandatory release-workflow runtime qualification, Apache-2.0 repository metadata, and combined `make p3-qualification`. Hosted OIDC/Ruleset evidence and current image scan results remain release-topology facts rather than repository claims.


## Task 118 — P4 go-live production readiness

Repository-complete: exact-tag `make p4-qualification`, GitHub applied-rules verification for a SHA-pinned required architecture workflow and Team reviewer, independent Sigstore/SLSA verification, staged GitHub Release asset-digest binding, non-secret production posture validation, live connector health/sync qualification, non-public draft staging and PASS-gated public promotion. A deployment may claim P4 only from retained evidence produced on the actual tagged release and production/hosting environment.

## Task 119 — UI product experience closure

Repository-complete: operator dashboard, SVG icon/navigation system, mobile accessibility repair, semantic design tokens/dark mode/density, toast/skeleton/drawer/dialog primitives, reusable searchable/sortable/paginated DataTable with bookmarkable views, focused Catalog/Orders/Inventory/Reconciliation workflows, warehouse incident and fulfillment-lineage UI, integration overview/setup drawers, onboarding, activity center, global command search, keyboard shortcuts and deterministic UX regression coverage. No backend/API contract changes are introduced.

## Task 120 — Enterprise Operations UX

Repository-complete: PostgreSQL-backed server DataGrid for Catalog/Orders, authenticated metadata-only SSE invalidation, unified Incident Center, durable entity deep links plus server-side Command Palette search, and reporting-backed professional analytics. OpenAPI is additive at 0.15.0 / 108 generated SDK operations; migrations remain at 74.

## Task 121 — Pre-v1 Migration Baseline / Squash

Repository-complete: fresh-install PostgreSQL migration inventory is compacted from the 74-file development chain to 11 active baseline files; the original 74 migrations/catalog are immutable archived evidence; deterministic baseline regeneration/equivalence is gated; existing exact-head development databases have an explicit verified one-time rebaseline that archives all old history before stamping the compact baseline. No business schema/data semantics are changed by the squash.

## Task 132 — Telegram Social production runtime

Repository implementation complete: the Task-041 Telegram adapter is composed
through Task-020 Social Core, authenticated channel/publication APIs, leased
worker delivery, append-only remote receipts and a dedicated `/social`
frontend. Production admission is limited to `social.post.text`; media,
buttons, edit/delete and inbound updates remain planned until their full host
workflows are connected. Final live-provider qualification remains pending
because the local environment has no Telegram account/bot token; it must use a
non-production bot and dedicated test channel before a deployment claims the
RUNTIME-132 live gate.

## Task 133 — MAX Social production runtime

Repository implementation complete: the Task-042 MAX adapter is composed
through the existing provider-neutral Social API, leased worker and append-only
receipt recovery. At the completion of Task 133, production admission was
deliberately `social.post.text` only with the exact 4000-code-point ceiling and
fixed `platform-api2.max.ru` egress. Task 175 subsequently adds released image,
gallery and supported-video publication through the host upload bridge; buttons,
status reads and webhooks remain SDK ceilings rather than application claims.
Live-provider qualification remains pending because this environment has no MAX
bot token or dedicated test channel.

Follow-up discovered during Task 133: OAuth-based connectors such as VK and
Avito must remain planned until the host refreshes expiring OAuth bundles and
passes only callback-scoped access tokens into adapters. Without this boundary,
those providers would force repeated sign-in and fail after token expiry.

## Task 134 — Host-owned OAuth refresh runtime

Repository implementation complete: account-aware Connector SDK runtimes now
project only current access-token bytes, refresh expiring authorization-code
bundles through exact manifest endpoints and rotate the same encrypted opaque
reference. PostgreSQL transaction advisory locks plus a post-lock bundle reread
prevent API/worker races against rotating refresh tokens. Client-credentials
grants exchange without a browser. No connector readiness count changes;
Task 135 may now compose VK on top of this boundary.

## Task 136 — Unauthenticated verified webhook ingress boundary

Repository implementation complete (ADR-0105): a new `PublicWebhookRoute`
table, registered alongside `ProtectedRoute` in `NewProductionHandler`,
carries inbound provider callbacks under the fixed `/api/v1/webhooks/` prefix
with its own rate-limit budget and body bound, entirely independent of
authenticated tenant traffic. It never runs the OIDC
Authenticator/TenantResolver/Authorizer chain and never populates
`PrincipalFromContext`/`ScopeFromContext` — the dispatched handler owns
resolving its own tenant scope and proving caller authenticity (per ADR-0105,
by re-verifying against the provider's own API rather than trusting a
signature header). This makes `sdk.PaymentWebhookVerifier` (implemented for
`yookassa`/`sbp` while building the payments core, previously unreachable)
reachable for the first time. No payment-specific handler is wired yet —
Task 137 does that on top of this boundary.

## Task 137 — Payment provider webhook receivers

Repository implementation complete: `POST
/api/v1/webhooks/payments/{connector_id}/{organization_id}/{workspace_id}/{account_id}`
is registered through Task 136's boundary. The handler resolves the account,
calls `registry.PaymentGateway(...).VerifyPaymentWebhook` inside the
account's own `UseSecret` scope, records replay-deduped
`payments.WebhookEvidence` (migration 000018's `payment_webhook_receipts`),
and applies the transition through the same `ValidatePaymentTransition`/
`ChangePaymentStatus` path every other payment mutation uses — the target
status comes only from the verified `EventType`, never from the request
body. A new `payments_remote_uq` index (migration 000019) and
`Repository.PaymentByRemoteID` resolve the local payment from the provider's
own remote id, which a webhook delivery is the first caller to only ever
have. Every outcome — unknown account, replay, verification failure, or a
real transition — returns the identical `200 {}` acknowledgement, so no
response-timing or status-code signal distinguishes them (ADR-0105). SBP
webhook delivery remains code-complete but unverified for the same reason as
its other operations: no real acquiring-bank gateway exists in this
environment.

## Task 156 — Categorical runtime surfaces

Repository implementation complete: the remaining 14 manifest-only providers
are grouped into «Объявления и вертикали», «Социальные сети», «ЭДО» and
«Госсистемы». Each has tenant-scoped credential enrollment and a bounded
authenticated health check through the host-mediated catalog probe. They expose
no product, publication, document, regulated-write or synchronization
capability until a separate provider qualification adds the required domain
bridge and worker route. Current inventory at the completion of Task 156 was
**18 generic / 38 separate-surface / 0 planned** across 56 providers; later
Tasks 157–159 expand the current inventory to **18 generic / 43 separate-surface
/ 0 planned** across 61 providers.

## Task 157 — М.Видео и Lamoda marketplace surfaces

Repository implementation complete: Lamoda and М.Видео are registered under
`connectors/marketplaces`, shown as branded cards in the «Маркетплейсы» tab and
admitted only to tenant-scoped credential enrollment plus a bounded operator-
configured HTTPS health probe. Their SDK manifests document the expected
marketplace vocabulary, but the runtime exposes zero domain capabilities and
zero product sync directions until current partner API qualification is
complete. Current inventory: **18 generic / 40 separate-surface / 0 planned**
across 58 providers.

## Task 158 — «Долями» payment surface

Repository implementation complete: «Долями» is registered under
`connectors/payments`, shown in the «Платежи» tab and admitted to a dedicated
mTLS/basic health-check surface. The partner login/password and certificate
remain encrypted and callback-scoped; the probe URL is operator-owned runtime
configuration. Payment creation, commit/cancel, refunds, status reads and
webhooks remain closed until a current Dolyami qualification package exists.
Current inventory: **18 generic / 41 separate-surface / 0 planned** across 59
providers.

## Task 159 — Google Gemini and Grok AI providers

Repository implementation complete: Google Gemini and Grok are available in
Settings → AI providers and the report AI selector. Gemini uses the official
`generateContent` REST contract with `x-goog-api-key`; Grok uses xAI Chat
Completions with a Bearer key. Both expose only bounded non-streaming text
completion through the existing governed AI surface. Midjourney is explicitly
not admitted because its official policy provides no general public API and
prohibits third-party automation. Current inventory: **18 generic / 43
separate-surface / 0 planned** across 61 providers.

## Task 170 — WMS operator workspace and marketplace fulfillment

Done for the repository slice: 170.1 → 170.7, 170.9 and 170.12 connect
canonical orders and durable fulfillment allocations to tenant-scoped WMS
execution tasks, standalone inventory work, bounded local pack handoff and the
operator UI. Marketplace write APIs, labels, Честный знак, external
shipment/status writes, automatic on-hand consumption and live production
qualification remain separately scoped gates.

## Task 172 — Yandex Market inventory write

Repository-complete: Yandex Market's provider-neutral `inventory.write`
capability is admitted through the generic commerce-sync worker. The adapter
selects the documented partner-warehouse or grouped-warehouse endpoint from
explicit host configuration, validates numeric warehouse scope and the
provider quantity bound, returns asynchronous acceptance as
`Applied=true, Reconciled=false`, and is covered by deterministic request and
failure tests. Product, order-status and other provider writes remain closed;
credentialed staging qualification is still a release-topology gate.
