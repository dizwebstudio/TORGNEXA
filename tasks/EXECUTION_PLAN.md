# Execution Plan

This is the canonical sequential linearization of the TORGNEXA backlog. A task starts only after all preceding mandatory dependencies are complete and closes only after its acceptance checks pass. Parallel implementation is allowed only after the relevant gate and must not concurrently change shared contracts.

Tasks `076`, `088`, and `089` are explicitly split into `a` and `b` implementation stages below. The numbered parent task closes only at stage `b`.

## Progress

## Current closure sequence — 2026-09-01

Работа над оставшимися карточками выполняется строго последовательно: в каждый
момент времени активна только одна задача. Статус задачи переводится в
`repository-complete` только после реализации, обновления API/контрактов и
документации, доступного frontend-сценария и прохождения релевантных проверок.

1. **Task 135 — VK production runtime composition — repository-complete.**
   Text publication is admitted through the shared Social Core surface;
   media/comments/analytics remain explicitly runtime-deferred.
2. **Task 163 — Workflow Automation Builder — repository-complete.** Runtime,
   recovery, quotas, API/UI и qualification checks закрыты; live-provider gates
   остаются в task card.
3. **Task 164 — Returns, cancellations and refunds — repository-complete.**
   Domain, tenant persistence, idempotent API, durable logistics route и
   operator UI закрыты; live WMS/fiscal/payment qualification остаётся gate.
4. **Task 165 — Forecast and auto-replenishment.** Закрыт repository slice:
   exact forecast/projection/recommendation, tenant persistence, API/OpenAPI,
   generated SDK и frontend `/replenishment`. Provider inputs, durable
   production worker и approved PO execution остаются release gates.
5. **Task 221 — Pricing, repricing, Buy Box and promotions — repository-complete
   for preview slice.** Deterministic preview, floor/max-step guards, API,
   SDK и frontend `/pricing` закрыты; marketplace apply/promotions/Buy Box
   остаются qualification gates.
6. **Task 222 — Listing content, attributes and mass editing — repository-complete
   for the current PIM/publication slice.** Catalog, content, offers, prices,
   categories, media, quality preflight и publication UI закрыты; provider
   taxonomy, batch apply и live read-after-write остаются gates.
7. **Epic 176 / Task 223 — final marketplace integration gate —
   repository-complete.** Связать
   результаты 164/165/221/222 с marketplace order, fulfillment, returns,
   settlement/P&L и operations center; затем отдельно провести provider
   qualification. До официального non-production smoke WB/Ozon остаются
   `read_only`/`partially_supported`; live provider evidence remains the release
   gate.

Task 135–222 выполняются по одному. Для каждого шага обязательны: task card,
product/architecture/operations documentation, OpenAPI and generated SDK when
the public contract changes, frontend route and operator workflow, tenant/RLS
and security checks, deterministic tests, and the appropriate migration,
contract, Docker and performance checks. Repository completion does not invent
live-provider evidence.

- Completed repository implementation: `001`, `024`, `065`, `002`, `027`,
  `067`, `080`, `003`, `021`, `060`, `007`, `008`, `009`, `004`, `005`, `006`, `076`, `025`, `010`, `029`, `064`, `017`, `030`, `023`, `081`, `082`, `028`, `026`, `063`, `022`, `062`, `032`, `031`, `088`, `013`, `014`, `011`, `012`, `015`, `016`, `033`, `034`, `035`, `036`, `018`, `079`, `020`, `019`, `078`, `040`, `041`, `042`, `037`, `038`, `039`, `043`, `044`, `045`, `046`, `047`, `048`, `049`, `050`, `051`, `052`, `053`, `054`, `055`, `056`, `057`, `058`, `059`, `061`, `066`, `068`, `069`, `070`, `071`, `072`, `073`, `074`, `075`, `077`, `083`, `084`, `085`, `086`, `087`, `090`, `091`, `092`, `089`, `093`, `094`, `095`, `096`, `097`.
- Completed split-stage repository implementation: `076a`, `076b`, `088a`, `088b`, `089a`, and `089b`; parent Tasks `076`, `088`, and `089` are repository-complete.
- Contiguous implemented baseline: Tasks `001`–`135`. Task `118` closes the P4 repository layer with fail-closed go-live evidence synthesis and PASS-gated release promotion; Tasks `119`–`130` add operator UX, compact migrations, AI/MCP governance, the trust control plane and a runtime-truthful integration catalog; Tasks `131`–`133` compose CBR FX, Telegram and MAX into truthful dedicated production surfaces; Task `134` closes the host-owned OAuth access-token projection and refresh boundary; Task `135` composes VK text publication on the shared Social Core surface. Deployment/hosted and live-provider evidence remains release-topology specific and cannot be inferred from repository completion.
- Post-baseline provider tasks `139`, `141`, `142`, `143`, `145`, `147`, `149`, `150`, `151`, `152`, `153`, `154`, `155`, `156`, `172`, `173`, `174`, `175`, `176`, `177` and `178` are repository-complete; their live credentials, external API contracts and production qualification remain environment-specific gates. Task `156` groups all former planned entries into explicit category surfaces and admits only health checks.
- Task `157` is repository-complete: Lamoda and М.Видео are visible in the Marketplace catalog as health-only surfaces with tenant-scoped API-key enrollment and bounded operator-configured HTTPS probes; product, price, stock and order operations remain qualification-gated.
- Task `158` is repository-complete: «Долями» is visible in Payments as an mTLS/basic health-only surface; payment mutations and webhooks remain qualification-gated.
- Task `159` is repository-complete: Google Gemini and Grok are visible in the governed AI-provider surface with official API-key transports; Midjourney remains intentionally unavailable because its terms prohibit third-party automation.
- Task `161` is repository-complete: `commerce-sync` now consumes canonical product change events, invokes admitted ProductWriter routes with provider-native status translation, and persists product mappings only after validated remote receipts.
- Task `162` is repository-complete: the Community deployment now has a repeatable authenticated Chrome E2E that reconciles the Keycloak demo member and verifies catalog, product images, orders and order thumbnails through the rendered browser UI.
- Task `135` is repository-complete for its admitted slice: VK OAuth, strict
  group configuration, health and Social Core text publication are composed
  through the common host transport and exposed in the Integrations and
  Publications frontend. Media, comments and analytics remain SDK-ready but
  fail-closed until their complete application workflows are added; live
  provider readiness still needs a dedicated non-production account.
- Task `163` is repository-complete for its implementation slice: the Workflow Automation Builder foundation and
  runtime are implemented; the remaining qualification gates are tracked in
  the issue and must pass before production readiness. The feature is decomposed into
  ten bounded subtasks covering the action catalog/ADR, immutable workflow
  versions, schema-backed DSL/compiler, RLS persistence, EventBus and durable
  schedule triggers, execution/retry/approval runtime, typed safe adapters,
  REST/UI, quotas/observability/recovery and load/chaos/Compose qualification.
- Task `164` is repository-complete for its bounded implementation slice: Returns, cancellations and refunds are decomposed into
  twelve bounded subtasks covering policy/state machines, order/payment/
  fulfillment/WMS orchestration, RLS persistence, canonical events and
  webhooks, cancellation and return workers, refund/fiscal/settlement
  reconciliation, REST/UI, connector qualification, operations and Compose
  release evidence. The existing payments refund lifecycle remains the single
  mutation path; ambiguous external outcomes require reconciliation/manual
  attention rather than blind retry.
- Task `165` is repository-complete for the bounded provider-neutral runtime:
  Stock forecasting and auto-replenishment are decomposed
  into thirteen bounded subtasks covering deterministic forecasts, data-quality
  gates, stock projections/risk, supplier/MOQ/budget optimization, RLS/lineage,
  scheduler/worker, guarded draft/submit PO execution, REST/UI, connector
  qualification, operations and Compose evidence. Task 053 remains the
  advisory baseline; forecast never becomes inventory truth and `auto_submit`
  cannot bypass procurement approval or the existing PO lifecycle.
- Task `166` is planned: the Product Publication Quality Center is decomposed
  into thirteen bounded subtasks covering target-specific readiness, immutable
  snapshots, declarative connector profiles, catalog/PIM/media/price/stock and
  compliance checks, deterministic score/rules, the `commerce-sync` preflight
  gate, durable worker, RLS/lineage, REST/UI, safe remediation/approval,
  connector qualification and Compose release evidence. Task 082 remains the
  final compliance guard; stale, unknown or unsupported quality evidence fails
  closed and never authorizes a remote write.
- Task `167` is planned: channel unit economics is decomposed into eighteen
  bounded subtasks covering accounting bases, channel identity, exact metric
  contracts, normalized source facts, historical COGS, allocation conservation,
  settlement/payment deduplication, ads/promotions, returns/refunds, sourced
  FX, immutable calculation runs, ClickHouse projections, RLS/lineage,
  worker/API/exports, operator UI, security/quotas and Compose qualification.
  Missing facts never become zero, payout never becomes revenue, and every
  cross-currency result requires Task-089b conversion evidence.
- Task `168` is complete: the Unified Integration State Center is implemented
  into eighteen bounded subtasks covering multidimensional account/runtime/
  credential/config/capability/health/sync/reconciliation state, bulk adapters,
  deterministic reduction, derived snapshots/RLS/lineage, events/worker/SSE,
  REST/OpenAPI, operator UI, idempotent actions, security/SLO/quotas and
  Compose/test/documentation qualification. GET performs no remote probe and
  manifest/health-only/SDK-only evidence never becomes executable green state.
- Task `223` is repository-complete for the provider-neutral control-plane and
  synthetic orchestration, with live provider qualification as the remaining
  release gate for user-facing Epic 176 Marketplace Operations v1. It composes the existing product,
  pricing/inventory, orders, WMS/fulfillment, returns, marking/EDO,
  settlement, advertising and financial boundaries; it does not claim a
  provider as fully qualified until the complete end-to-end gate passes.
- Tasks `025`, `010`, `029`, and `064` are repository-complete; Connector SDK major v1, plugin security, dry-run/test sandbox, and mandatory conformance suite are closed. Task `011` is the later provider-admission change: repository policy now registers the first read-only provider after all four prerequisites; hosted trusted-base qualification still requires the prerequisite-status parser normalization to exist in the merge base before the protected admission PR.
- Operational release qualification still blocked: `065` (`SC-OPS-01` protected OIDC prerelease evidence and current runtime-image findings). The repository license decision itself is resolved as Apache-2.0.
- Operational architecture qualification still blocked: `080`
  (`ARCH-OPS-01`, protected Required Workflow/reviewer/branch-policy evidence).
- Repository Gate F0 is complete through Task `060`; stage `076a` plus Tasks `007`, `008`, and `009` repository implementations are complete; the EventBus/outbox/inbox correctness chain is closed; Tasks `004` Catalog, `005` Price + Inventory, `006` Orders, and final audit stage `076b` are repository-complete; stage `089a`, Task `017`, Task `030`, Task `023`, Task `081`, Task `082`, Task `028`, Task `026`, Task `063`, Task `022`, Task `062`, Task `032`, stage `088a`, Task `031`, stage `088b`, Task `013` Sync Engine and Task `014` Reconciliation are repository-complete; repository Gate F1 is closed; Tasks `011` Wildberries Connector, `012` Ozon Connector, `015` 1C Connector, `016` MoySklad Connector, post-reference Task `033` Yandex Market Connector and Tasks `034` Megamarket Connector, `035` Magnit Market Connector and `036` AliExpress RU Connector are repository-complete; repository Gate F2 reference-connector evidence is closed; Tasks `018` MCP, `079` AI Agent Governance, `020` Social Core, `019` n8n Node, `078` Plugin Marketplace Governance, `040` VK Connector, `041` Telegram Connector, `042` MAX Connector, `037` Avito Connector, `038` Auto.ru Connector, `039` CIAN Connector, `043` Instagram Connector, `044` Threads Connector, `045` OK Connector, `046` Rutube Connector, capability-audit Task `047` Dzen Connector and `048` YouTube Connector are repository-complete; Dzen deliberately has no live provider admission. The Phase-5 extended-channel branch is closed; Task `049` ClickHouse Reporting Foundation, Tasks `050`–`059`, Task `061` Retention / Subject Requests / Tenant Deletion, and Task `066` SLO / Performance Test Suite are repository-complete. Task 061 executes Task-060 policy through resumable authoritative/derived/object-store targets with legal-hold and append-only evidence semantics; Task 066 adds executable SLO budgets and deterministic failure/load qualification. Task `089b` is repository-complete: immutable sourced historical FX, CBR reference-provider admission, explicit staleness/cache policy, persisted resolution/conversion evidence and exact arithmetic now qualify cross-currency derivations. Tasks `068`–`075` are repository-complete: marking read/status, isolated signing/MЧД, Diadoc/Saby EDO, fiscalization abstraction, VetIS/Mercury, SBP payments, carrier SDK and PUDO operations are closed. Final Tasks `077`, `083`–`087`, and `090`–`092` are repository-complete, and post-backlog Task `093` adds the Community Docker deployment with canonical migration ordering, Keycloak and S3-compatible storage, Task `094` admits WooCommerce as the first bidirectional storefront connector, Task `095` adds PrestaShop, and Task `096` adds OpenCart through the versioned TORGNEXA bridge contract, and Task `097` adds the provider-neutral CRM family plus Bitrix24 using the universal CRM API. 
- No release candidate or release-dependent task may claim readiness while an
  earlier operational gate remains open. Ordinary repository development may
  continue after the deterministic local implementation gate passes, so an
  external hosting qualification does not serialize the entire backlog.

Task `065` has `public_release_ready:true` because Task 117 resolved the repository license as Apache-2.0. Task 118 now stages release bytes as a non-public draft and independently verifies OIDC/Sigstore/SLSA plus GitHub asset digests before final promotion. This is still not operational acceptance until that protected flow actually passes for the real tag/topology.

## Phase 30 — М.Видео и Lamoda marketplace surfaces

`157`

Task 157 adds branded Lamoda and М.Видео cards to «Интеграции →
Маркетплейсы». Both providers use the generic account/SecretProvider path and
health-only catalog probe; no unqualified marketplace domain operation is
advertised.

### Gate RUNTIME-157

- generated catalogs and runtime support report 58 providers: 18 `ready`, 40
  `separate_surface`, 0 `planned`, including two marketplace health-only rows;
- cards expose API-key enrollment, operator HTTPS probe configuration and a
  clear «Проверка подключения» state;
- API and worker reject product/price/inventory/order capability enablement for
  both providers;
- manifests, conformance evidence, policy/reviews and provider docs are in
  sync; no migration or public API change is required;
- domain activation waits for a current partner test account, fixtures,
  idempotent bridge and a fresh provider qualification review.

## Phase 31 — «Долями» payment surface

`158`

Task 158 adds «Долями» to «Интеграции → Платежи». The card stores the partner
login/password and mTLS certificate in SecretProvider and uses a one-shot
host-mediated client against the operator-configured endpoint. The runtime
inventory is **18 generic / 41 separate-surface / 0 planned** across 59
providers. Create, Commit, Cancel, Info, Refund and webhook operations remain
closed until provider qualification.

### Gate RUNTIME-158

- the card and generated runtime support are `separate_surface/finance/health_only`;
- mTLS private material is callback-scoped and never retained by a pooled client;
- runtime configuration contains only the HTTPS probe URL, not credentials;
- no payment capability is executable or selectable from the account;
- official API fixtures, signature verification and idempotent payment routing
  are required before promotion to a payment gateway.

## Phase 32 — Google Gemini and Grok AI providers

`159`

Task 159 adds Google Gemini and Grok to the existing tenant-scoped AI analytics
surface. Gemini uses `x-goog-api-key` and `generateContent`; Grok uses xAI's
Bearer-authenticated Chat Completions endpoint. Midjourney is documented as
unavailable because its official policy does not provide a general API and
prohibits third-party automation.

### Gate RUNTIME-159

- generated catalogs and runtime support report 61 providers: 18 `ready`, 43
  `separate_surface`, 0 `planned`;
- API, UI and migration provider allow-lists include `gemini` and `grok`;
- credentials remain callback-scoped and only `ai.completion.generate` is
  executable;
- package tests, conformance evidence, SDK generation and frontend checks pass;
- streaming, tools, image/video generation and Midjourney automation remain
  outside the admitted contract.

Task `027` has a completed deterministic repository drill, not a qualified
deployment backup chain. Each release/deployment must additionally retain a
successful restore against its real encrypted immutable store, KMS,
credentials, WAL archive, and topology within the selected RPO/RTO.

Task `067` has a completed deterministic catalog, runner, PostgreSQL adapter,
and upgrade rehearsal. A release must still repeat the rehearsal from every
supported deployed source version against that environment's topology,
extensions, backup checkpoint, data invariants, and mixed-version fleet.

Task `080` has a completed deterministic repository policy, static/diff
checker, governance contract, trusted-base workflow implementation, and
adversarial tests. Its repository workflow is not itself a hosting trust
anchor: `ARCH-OPS-01` remains blocked until an external Ruleset Required
Workflow (or equivalent immutable check), reviewer, and branch policy are
proved on a post-merge pull request. This external qualification does not
serialize ordinary repository development, but no hosted architecture-gate
readiness may be claimed before it passes.

## Phase 0 — Foundation and irreversible guardrails

`001 → 024 → 065 → 002 → 027 → 067 → 080 → 003 → 021 → 060`

- `024` protects all subsequent API, event, and schema changes.
- `065` establishes supply-chain gates before the codebase and dependency graph grow; its checks repeat for every release candidate.
- `065` has separate repository and operational states. Local completion allows
  foundation development to continue; release qualification closes only after
  the external protected-prerelease evidence and all release-input gates pass.
  Mock signing, workflow linting, or a local PASS alone is insufficient for
  publication.
- `027` follows the first working PostgreSQL tenant schema.
- `067` establishes expand/migrate/contract and resumable migration rules before further migrations accumulate.
- `080` is an early CI/review gate, not a final architecture ceremony.
- `003 → 021 → 060` establishes append-only audit, secret isolation, and privacy metadata before PII and external credentials appear.

### Gate F0

Build, test, vet, and contract checks pass. Backup/restore and upgrade rehearsal are reproducible. Tenant isolation, audit redaction, secret references, and the privacy registry are tested.

## Phase 1 — Correctness core and extension boundary

`076a → 007 → 008 → 009 → 004 → 005 → 006 → 076b → 089a → 017 → 030 → 023 → 081`

`025 → 010 → 029 → 064`

`088a → 031 → 088b`

`082 → 028 → 026 → 063 → 022 → 062 → 032`

- EventBus, outbox, and inbox precede production-grade mutating domains.
- `076a` defines Money, Decimal/Quantity, UTC, locale, address, and tax primitives before Tasks `004–006`; `076b` audits the frozen-Core mirrors, storage and public/event schemas against those canonical representations and closes Task `076`. Both stages are repository-complete.
- `081` is complete: ERP, EDO/MChD, procurement, payments, and compliance now have canonical LegalEntity, IndividualEntrepreneur, Branch, Counterparty, BankAccount, Contract and AuthorityReference identities.
- `025`, `010`, `029`, and `064` are complete: SDK v1 is stable, the signed sandbox/dry-run boundary is executable, and mandatory conformance is machine-verifiable. Provider admission stays disabled in this completion change because Task 080 requires `064` to be completed in the merge base before admission is opened.
- `010 + 029 + 064` form the mandatory connector runtime, dry-run, and conformance boundary; this implementation chain is repository-complete.
- `088a`, `031`, and `088b` are repository-complete: upload bytes remain quarantined until bounded MIME/archive/parser checks and fail-closed malware scanning produce immutable CLEAN evidence. Release references bind evidence/version and are revalidated before every downstream read; re-scan revokes stale references before scanner work. Parent Task `088` is closed at repository level.
- `017` is complete: sensitive/legally-significant writes now fail closed without a matching versioned policy and immutable multi-stage approval evidence.
- `030` is complete: price/stock mutations now commit immutable same-tenant lineage references linked to their audit/outbox evidence, and a bounded authenticated-scope timeline read contract is available.
- `023` is complete: canonical Brand/Category/Attribute masters, product master-data assignments, field-authority rules, explainable duplicate candidates, deterministic non-executing merge previews, and additive external mapping v3 are repository-qualified.
- `082` is complete: versioned product-compliance evidence/policies, registry verification/expiry ports, explainable evaluation, and a host-side fail-closed `products.write` publication guard are repository-qualified before connector write.
- `028` is complete: tenant/workspace feature rules and UTC-window quotas are provider-neutral, deny-by-default, versioned, RLS-protected and atomically consumed without subscription-plan branches. Task `086` may synchronize these records later but Community runtime is independent.
- `026` is complete: the provider-neutral SearchProvider now performs authenticated tenant/workspace Product/Order search through PostgreSQL FTS/prefix indexes, explicit tenant predicates, forced RLS and query-bound keyset cursors without introducing an external search source of truth.
- `063` is complete: tenant-scoped durable webhook subscriptions now enqueue immutable request snapshots from EventBus deliveries, sign each attempt through Task-021 secret references, retry with bounded backoff/leases, retain immutable history/DLQ/replay evidence, support bounded signing-key overlap, and re-resolve HTTPS destinations on every attempt with fail-closed SSRF controls.
- `022` is complete: the canonical tenant-scoped notification inbox now deduplicates repeated conditions per authenticated recipient, tracks monotonic severity/occurrences/read state, enforces per-channel preferences and immutable delivery outcomes, defaults external webhook delivery to opt-in, and delegates webhook egress to Task `063`.
- `062` is complete: public Go, TypeScript and Python REST clients are generated deterministically from OpenAPI only, drift-checked by source hash/operation inventory, and forbidden from exposing internal database models.
- `032` is complete: the React/TypeScript/Vite shell consumes the generated TypeScript SDK, keeps OIDC access-token material memory-only behind a host auth adapter, enforces capability-aware navigation/direct-route guards, and provides API-backed Catalog, Orders and Notifications screens without client tenant selectors or provider-specific branches.
- `031` is complete: released-object SHA-256/size is verified before preview and again before commit, mappings are versioned/fingerprinted, invalid previews fail closed, Product replay is idempotent, and CSV/JSON export encoding is deterministic.
- `013` is complete: provider-neutral inbound/outbound/bidirectional policies, tenant-scoped durable checkpoints/state/receipts, deterministic idempotency, mapping-based identity, correlation/causation, policy-scoped loop suppression, payload-fingerprint echo detection and local/remote/manual conflict tests are repository-qualified.
- Repository Gate F1 is complete. Task `088` now enforces quarantine-before-scan, immutable security evidence, fail-closed scanner behavior, revocable release references and consumer revalidation. Tasks `013` Sync Engine, `014` Reconciliation, `011` Wildberries Connector, `012` Ozon Connector, `015` 1C Connector, `016` MoySklad Connector, and `033` Yandex Market Connector are repository-complete; the reference integration chain plus the first post-reference marketplace connector are closed and Tasks `018` MCP, `079` AI Agent Governance, `020` Social Core, `019` n8n Node, `078` Plugin Marketplace Governance, `040` VK Connector, `041` Telegram Connector, `042` MAX Connector, `037` Avito Connector, `038` Auto.ru Connector, `039` CIAN Connector, `043` Instagram Connector, `044` Threads Connector, `045` OK Connector, Task `046` Rutube Connector and Task `047` Dzen audit/transformer are repository-complete; Task `049` ClickHouse Reporting Foundation and Task `089b` FX Rate Provider completion are repository-complete; final Tasks `077`, `083`–`087`, and `090`–`092` are repository-complete and the numbered backlog is closed.
- `014` is complete: one durable runner now supports incremental/scheduled-full/on-demand reconciliation, records six bounded drift classes under forced RLS, resumes from persisted cursors, and restricts auto-fix to unambiguous mapping creation or Task-013 authority/direction-safe repair; notify/approval/ignore remain explicit append-only actions with deterministic idempotency.
- `011` is complete: Wildberries is the first registered read-only marketplace provider, current official Content/Marketplace read surfaces are isolated behind the Connector SDK transport boundary, deterministic fixtures cover product/warehouse/stock semantics, and the canonical Task-064 report passes 13/13 checks.
- `012` is complete: Ozon is the second registered read-only marketplace provider, reuses the same SDK-v1 Product/Inventory read interfaces, isolates current Seller API product/warehouse/FBS-stock surfaces behind the host transport boundary, and passes the canonical Task-064 13/13 report.
- `015` is complete: 1C is the first registered ERP provider, uses a configured standard OData read-only baseline with exact decimal balances and configuration-bound cursors, keeps 1C identities in Task-010 mappings, and reuses Task-013/014 reconciliation without introducing ERP identifiers into Core.
- `016` is complete: MoySklad is the second registered ERP provider, reads product assortment, stock-by-store and customer orders through the official JSON API, adds only the additive `erp.orders.read` SDK capability, and passes deterministic fixtures plus the Task-064 conformance suite without changing frozen Connector/Runtime roots.
- `033` is complete: Yandex Market is the third registered marketplace provider, reads products/prices/inventory/orders and normalizes inbound API notifications through current Partner API surfaces, keeps business/campaign/offer/order/warehouse identifiers in mapping/configuration boundaries, grants no marketplace writes, and passes the Task-064 13-check conformance suite.
- `034` is complete: Megamarket is the fourth registered marketplace provider, uses only official merchant API catalog/stock/order read surfaces with explicit DBS/FBO configuration, keeps goods/offer/shipment/warehouse identities outside Core, grants no writes, and passes Task-064 conformance.
- `035` is complete: Magnit Market is the fifth registered marketplace provider, uses the current official Partner API for shop-scoped SKU catalog, exact prices, explicit aggregate stock-type inventory and FBS orders, keeps product/SKU/order/shop identities outside Core, grants no writes, and passes Task-064 conformance.
- `036` is complete: AliExpress RU is admitted strictly from the Russia-facing seller path, grants only `products.read`, uses JWT `X-Auth-Token` through the secret boundary, preserves `ali_updated_at`/product/SKU remote identities, explicitly ignores deprecated stock fields, defers unqualified stock/price/order capabilities, and passes Task-064 conformance.
- `018` is complete: stateless MCP `2026-07-28` exposes permission-filtered Product/Order/Counterparty reads plus an idempotent `commerce.price.change.request` that can only create/replay the exact Task-017 sensitive approval request; tenant scope comes only from authenticated identity and authorized calls fail closed when audit evidence cannot be recorded.
- `079` is complete: immutable per-agent/integration tool policy, hard action/frequency limits, tenant/agent/integration kill switches, bounded provenance and executable prompt-injection regressions are enforced directly in MCP. The AI-governance publication gate is closed; Task `084` still owns production federated identity/control-plane wiring.
- `020` is complete: provider-neutral Content masters, immutable publish variants, social ChannelAccount capability projections, host-owned UTC scheduling, guarded publication status/attempt history, released UploadID media references, atomic audit/outbox evidence and additive SDK-v1 social publish/status/media interfaces are repository-qualified. Task `019` consumes these public/core boundaries without embedding n8n.
- `019` is complete: the separate `n8n-nodes-torgnexa` package exposes identity-scoped Product/Order reads and an exact-body verified webhook trigger through public REST only; workflow deactivation idempotently disables the durable Task-063 subscription and revokes signing material without deleting delivery evidence.
- `078` is complete: reviewed public/private plugin listings expose trust and requested authority, every new artifact requires exact-digest tenant consent, privilege growth is explicitly surfaced, and artifact/publisher-key/installation revocations fail closed before the existing Task-025/029 admission chain.
- `040` is complete: VK is the first Social Core provider, using canonical Publication identity for retry-safe wall posts, Task-088 released media, group-scoped comments and bounded post-reach analytics; video/edit/delete remain undeclared.
- `041` is complete: Telegram is the second Social Core provider, with exact-channel bot authorization, Task-088-backed text/photo/album/video publication, HTTPS URL buttons, bounded single-message edit/delete, provider `retry_after` handling and non-retryable ambiguous-write outcomes.
- `042` is complete: MAX is the third Social Core provider, using exact-channel bot authorization, Task-088-backed text/image/gallery/video publication, HTTPS URL buttons, read-after-publish status and production Webhook secret verification plus host-owned durable dedup. Tasks `043` Instagram, `044` Threads and `045` OK are also repository-complete. Task `047` Dzen is repository-complete as an audit/transformer with no live provider admission; Tasks `046` Rutube and `048` YouTube are also repository-complete; Task `049` ClickHouse Reporting Foundation, Tasks `050`–`059`, and Task `089b` FX Rate Provider completion are repository-complete; the canonical next task is `077` Incident Management & Runbooks.
- `043` is complete: Instagram is the fourth Social Core provider, binds an exact professional Business/Creator account, admits media/Reels publication through Task-088 revalidation plus short-lived HTTPS staging, and fails closed on unsupported text-only/provider-specific surfaces.
- `044` is complete: Threads is the fifth Social Core provider, binds an exact user, admits text/image/video publication, and keeps long-lived token exchange/refresh behind Task-021 secret references plus a host-owned token sink without changing Core. Task `045` OK is repository-complete. Task `047` Dzen is repository-complete without live provider admission. Tasks `046` Rutube and `048` YouTube are also repository-complete. Task `049` ClickHouse Reporting Foundation, Tasks `050`–`059`, and Task `089b` FX Rate Provider completion are repository-complete; the canonical next task is `077` Incident Management & Runbooks.
- `045` is complete: OK/Odnoklassniki is the sixth Social Core provider, binds one exact group, signs official REST calls with Task-021-scoped OAuth/application secrets, publishes text/photo/video group media topics, reads exact status and bounded topic analytics, and fails closed on ambiguous writes.
- `047` is complete without provider admission: the current audit did not establish a qualified official public Dzen publishing contract, so TORGNEXA adds only deterministic post/article/video content transformation and an explicit fail-closed live-publication gate; private Studio/editor endpoints, browser cookies, DOM automation and undocumented RPCs remain forbidden.
- `048` is complete: YouTube is the eighth admitted Social Core provider, with exact-channel OAuth binding, Task-088 video revalidation, official resumable upload semantics with status probes/confirmed offsets, bounded processing reconciliation and account-scoped top-level comment reads. Comment writes, native scheduling and analytics remain deliberately undeclared where the frozen SDK cannot preserve idempotency or reporting semantics.

### Gate F1

No provider connector starts before stable Tasks `010`, `024`, `025`, `029`, and `064`. Uploads reach downstream consumers only through a current Task-088 CLEAN/RELEASED capability that is revalidated before use. Sensitive writes require Task `017`, lineage/audit, idempotency, and risk classification.

## Phase 2 — Architecture proof and v1 reference integrations

`013 → 014 → 011 → 012 → 015 → 016`

`018 → 079 → 020 → 019 → 078`

`033 → 040 → 041 → 042`

- Sync and reconciliation are first verified with fake/emulator adapters, then with WB and Ozon references.
- WB and Ozon must pass conformance and prove that the SDK remains provider-neutral.
- ERP uses stable reconciliation and legal-party abstractions.
- The MCP AI-governance gate is closed by `018 → 079`; production exposure remains gated by `084` trusted Enterprise IAM/control-plane composition.
- Yandex Market, VK, Telegram, MAX, Instagram, Threads and OK are repository-complete as post-reference channel integrations; Task `047` Dzen is also audit/transformer-complete without provider admission. The extended-channel branch is closed through `048` YouTube; Task `049` ClickHouse Reporting Foundation, Tasks `050`–`059`, Task `061`, Task `066`, and Task `089` are repository-complete; sourced historical cross-currency derivations are now enabled only through immutable Task-089b conversion evidence.

### Gate F2

WB, Ozon, and ERP reference connectors pass deterministic tests and conformance. Duplicate delivery, crash/retry, loop prevention, and drift scenarios pass. Connector SDK v1 may then be treated as stable.

## Phase 3 — Finance, retention, and production hardening

`049 → 058 → 059 → 089b → 061 → 066 → 077 → 084 → 085 → 092`

- Tasks `049`, `058`, and `059` remain authoritative in original source currency; cross-currency presentation/reconciliation is permitted only through immutable Task-089b conversion evidence.
- `089b` is repository-complete: immutable sourced rates, storage/provider adapters, staleness policy, resolution evidence and reproducible historical conversion close Task `089`.
- Retention/deletion follows the PostgreSQL, search, ClickHouse, and settlement stores it must govern.
- Task `061` retention/deletion and Task `066` executable SLO/performance qualification are repository-complete.
- Runbooks, Enterprise IAM, SIEM, and edge security close before production or cloud readiness.

### Gate F3

There are no unsourced cross-currency calculations. Restore, upgrade, and incident procedures are executable. Security-event export is asynchronous and redacted. Forwarded headers are ignored outside configured trusted proxies.

## Phase 4 — Growth, supply chain, and customer operations

`051 → 050 → 052 → 053 → 054 → 055 → 074 → 075 → 090 → 056 → 057 → 091`

- Pricing guards precede advertising automation.
- Procurement uses canonical Task `081` legal-party data.
- WMS preserves ledger semantics.
- The reference logistics connector follows carrier and PUDO abstractions immediately.
- Claims and evidence consume released uploads only.
- SMS inherits notification, privacy, webhook, quota, and marketing-consent policies.
- Tasks `050`–`057`, `074`, `075`, `090`, and `091` are
  repository-complete. The reference logistics and SMS work is closed at
  repository level; deployment-specific credentials and provider qualification
  remain release concerns.

### Gate F4

Inventory and settlement histories are append-only. Recommendations retain their input snapshot and algorithm version. The carrier reference passes conformance. Customer communication has explicit retention and consent controls.

## Phase 5 — Extended channels

`034 → 035 → 036 → 037 → 038 → 039 → 043 → 044 → 045 → 046 → 047 → 048`

Repository status: `034`, `035`, `036`, `037`, `038`, `039`, `040`, `041`, `042`, `043`, `044`, `045`, `046`, `047`, and `048` are complete. The classified sequence is closed through CIAN; Instagram, Threads, OK, Rutube and YouTube are closed; Dzen is audit/transformer-complete without provider admission. Phase 5 is repository-complete.

Every provider requires a fresh official API/capability audit, Connector Spec, deterministic mocked tests, and conformance report. Every write passes capability, approval, dry-run/idempotency, and Product Compliance checks.

## Phase 6 — Regulated systems, payments, and Cloud

`068 → 069 → 070 → 071 → 072 → 073 → 083 → 087 → 086`

- Chestny ZNAK begins with read/status/reconciliation behavior.
- Signing/UKEP/MChD precedes EDO and EGAIS.
- EDO, payments, and EGAIS use Task `081`; product operations use Task `082`.
- Tasks `068`–`075`, `083`, `087`, and `086` are repository-complete; Phase 6
  is closed at repository level.
- The acquiring reference proves PaymentProvider before Cloud billing.
- Cloud billing remains separate from commerce payments.

## Phase 7 — Community packaging

`093`

Task `093` is repository-complete: one multi-stage TORGNEXA application image
runs API/worker/scheduler/MCP, while Compose adds canonical migrations, Keycloak
and S3-compatible Garage to the PostgreSQL/Kafka/Valkey/ClickHouse baseline.
The frontend lockfile is now committed, and Community Compose also
runs the loopback-only frontend development container used by later application
tasks. Local secrets are generated and application containers remain
non-root/read-only/capability-free. Production frontend publication remains
disabled by the JavaScript artifact policy.

### Gate F7

`make community-check` passes, migration deployment metadata matches the canonical catalog byte-for-byte by SHA-256, and the Compose topology remains explicitly single-host/non-HA.

## Phase 8 — Storefront integrations

`094 → 095 → 096`

Task `094` is repository-complete: WooCommerce is admitted through current `wc/v3` REST semantics with exact HTTPS store binding, callback-scoped credentials, bounded product/variation/price/inventory/order/refund reads, provider-neutral desired-state commerce writes, ambiguity reconciliation and verified/replay-safe webhooks. Root Connector/Runtime SDK v1 remains frozen; new commerce write/return/webhook interfaces are additive only.

Task `095` is repository-complete: PrestaShop is admitted through the native Webservice API. Products/combinations, exact prices, StockAvailable inventory and orders are read through bounded resource queries; price/inventory/order-state writes use XML mutation bodies plus read-after reconciliation. Product authoring, promotions, returns and webhook semantics remain deliberately deferred.

Task `096` is repository-complete: OpenCart is admitted through a versioned shop-local `extension/torgnexa/api/*` bridge contract. TORGNEXA never uses OpenCart admin sessions or remote DB credentials; product create reconciles by SKU and all exact-state writes reconcile before success. OpenCart option-authoring, returns/webhooks and signed Marketplace packaging remain separate follow-up work.

### Gate F8

WooCommerce retains its recorded Task-064 13/13 provider evidence. PrestaShop and OpenCart deterministic connector tests pass and each records Task-064 13/13 evidence, including Linux namespace/chroot sandbox isolation with the statically linked emulator. Credentials remain callback-scoped, ambiguous writes fail closed, and customer billing/shipping PII is not projected into canonical order models.

## Phase 9 — CRM integrations

`097`

Task `097` is repository-complete: Bitrix24 is the first provider in the additive `crm` family. Leads, deals, contacts and companies use universal `crm.item.*` methods; lead/deal product rows use `crm.item.productrow.*`. OAuth tokens remain callback-scoped Bearer credentials, creates reconcile on TORGNEXA origin identity, and ambiguous writes fail closed unless desired remote state can be proven. Contact `fm` multifields, event subscriptions and OAuth refresh/install flows are deliberately deferred.

### Gate F9

Architecture validation passes with 109 modules / 32 providers / 99 reviews. Bitrix24 retains Task-064 **13/13 PASS** conformance evidence including sandbox isolation.

## Phase 10 — Application settings and public documentation

`098 → 099 → 100 → 101 → 103`

`098 → 102`

`098 → 104 → 105 → 106 → 107 → 108 → 109`

`110`

- `098`–`108` and `110` are implemented.
- `106` adds manifest-declared OAuth endpoints/scopes and remote probes,
  PKCE S256, exact callback/actor/version binding, one-time encrypted state and
  DNS-pinned bounded production validation.
- `101` is implemented with default-deny issuer egress, SecretProvider-backed
  credentials, immutable revisions, validation-before-activation and audited
  rollback. `109` is implemented with bounded tenant-scoped health history and remediation categories.
- Task `110` exposes the unauthenticated Russian-language `/docs` application
  guide and repository-owned synthetic screenshots.
- Task `112` retires the qualified empty legacy `inbox_events` table through
  contract migration `000064`; canonical consumer deduplication remains in
  tenant-scoped immutable `inbox_receipts`.
- Task `113` replaces the placeholder worker wait loop with supervised PostgreSQL/Kafka/webhook/reconciliation/upload runtime composition and adds tenant-safe durable worker leases in migration `000067`.
- Task `114` closes the P0 runtime gaps: six priority product reconciliation source bridges, a production ActionExecutor, Kafka Inbox/idempotency wrapping, versioned non-secret runtime configuration in migration `000068`, and a green frontend shell gate.
- Task `115` closes P1 operations: connector health history, production notification adapters, privacy execution, and persistent safe warehouse failover in migrations `000069`–`000072`.
- Task `116` closes P2 repository qualification: migration `000073` durable warehouse incident automation, deployed-image Outbox→Kafka→Inbox duplicate/recovery probes, bounded API load/failure drills, and qualified Yandex Market `prices.write`. Runtime PASS evidence remains mandatory on the exact release topology.
- Task `117` closes P3 repository execution: migration `000074` durable order-item fulfillment allocations, atomic source-release/destination-reserve failover, allocation lineage/outbox events, Apache-2.0 release metadata, and a mandatory runtime-qualification job in the release DAG.
- Task `118` closes P4 repository go-live control: exact-tag qualification, GitHub applied-rules/required-workflow evidence, independent release identity verification, staged-asset digest binding, live connector qualification and PASS-gated draft promotion. No new database/API contract is introduced.

### Gate F10

Settings mutations remain tenant-derived, idempotent, capability-guarded and
audited. Connector activation stays fail-closed until credentials, declared
capabilities and remote health are validated. Public documentation contains no
tokens, credentials or private tenant data.

## P4 go-live gate

`118` requires an exact tagged release and combines P3 topology/restart/restore evidence with hosted GitHub rules, protected signed-release evidence, reviewed production posture and live connector health. A successful repository build is insufficient; only retained `p4-go-live.json: PASS` may authorize `make p4-publish`.

## Final gate

Before a Community or Enterprise release, repeat Tasks `024`, `027`, `065`, `067`, `066`, `093`, and connector conformance. Before Cloud production, Tasks `084–086`, `088`, and `092` are mandatory. Critical security findings, missing or unverifiable release evidence, a failed protected OIDC/signing qualification, failed restore/upgrade rehearsal, or conformance failure block release. The repository license decision is Apache-2.0 and `public_release_ready:true` now means only that license metadata no longer blocks the release; every independent security, provenance, hosted-policy, restore/upgrade and runtime qualification gate still fails closed.

## Required task decomposition

- `076a`: fixed-decimal Money/Quantity, currency validation, UTC/timezone-edge behavior, and locale/address/tax ports and contracts.
- `076b`: audit Tasks `004–006`, migrations, compatibility, and locale/tax tests; closes Task `076`.
- `088a`: UploadID/state machine, tenant-scoped quarantine/release ports, policy contract, and fail-closed access gate.
- `088b`: MIME/archive/parser/malware checks, scan evidence, re-scan behavior, metrics, and end-to-end tests; closes Task `088`.
- `089a`: immutable FXRate contract/provider port, deterministic source precedence, rounding/triangulation rules, and snapshot requirements; repository-complete with conversion intentionally disabled.
- `089b`: storage, reference adapter, cache/staleness/reconciliation, and historical reproducibility; closes Task `089`.

## Phase 11 — UI product experience

`119`

Task `119` is repository-complete. The frontend retains ADR-0047 identity/API/security boundaries while replacing the original engineering/admin presentation with an operator-oriented commerce-orchestration experience: operational dashboard, labelled icon navigation, responsive mobile shell, semantic tokens/dark/density modes, toast/skeleton/drawer/dialog primitives, reusable DataTable, focused entity/drift/incident workflows, integration setup drawers, onboarding, activity center, global capability-aware search and keyboard shortcuts. Bookmarkable table views are URL-only and do not persist tenant data in browser storage.

### Gate F11

`./scripts/check-frontend-shell.sh` must pass the original auth/capability/decoder tests plus Task-119 UX regressions. Browser token/tenant persistence and handwritten provider-specific branching remain forbidden. Task 119 adds no migration or public API operation and must not change P4 go-live qualification semantics.

## Phase 13 — Enterprise Operations UX

`120`

Task 120 is repository-complete: core commerce lists use server-owned query/filter/cursor pages, the shell consumes protected metadata-only SSE invalidation, incidents are composed into one operator queue, Catalog/Orders/Incident details have durable routes, product/order command search is server-side, and Dashboard/Reports use reporting-backed KPIs plus accessible SVG analytics.

### Gate UX-120

- frontend experience tests: 23/23;
- generated SDKs: 108 operations, OpenAPI 0.15.0;
- no new migration/event schema;
- realtime must remain metadata-only and protected by normal OIDC/tenant/RBAC composition;
- a release host with pinned Go 1.26.5 must run the API realtime handler test as part of the canonical Go suite.

## Phase 14 — Pre-v1 migration baseline

`121`

Task 121 is repository-complete. Fresh installs execute 11 active migrations (`000001`–`000011`) while the original development lineage `000001`–`000074` remains immutable under `migrations_legacy_pre_v1/`. The compact groups preserve legacy statement order, remove per-source history inserts, and write exactly one active history record per baseline file.

### Gate DB-121

- `make migration-baseline` must prove 11 active SQL files, 74 archived source files and exact archived checksums;
- `make migrations` must validate the compact active catalog and deterministic baseline generation;
- Community deployment must verify both active and legacy deployment metadata catalogs;
- an existing 74-row pre-v1 development database must fail closed unless `migration-rebaseline` first verifies all legacy version/name/checksum rows and final-schema sentinels;
- rebaseline must archive all 74 rows before stamping the 11 active baseline rows;
- after the first production v1 release this rebaseline path is retired and applied migration history is immutable.

## Phase 15 — Runtime-truthful integration catalog

`130`

Task 130 is repository-complete. Connector manifests remain the SDK inventory,
while `contracts/connectors/builtin-runtime-support-v1.json` is the exact source
of truth for production availability. Settings and the API expose only 11
generic integrations as executable, direct six AI connectors to their
dedicated surface and keep 21 manifest-only connectors visibly planned but
non-connectable. AliExpress RU, Magnit Market, Megamarket, OpenCart and
PrestaShop join the built-in runtime. PrestaShop additionally has an explicit
outbound worker route for `prices` and `inventory`; all other generic sync
routes remain product-scoped.

### Gate RUNTIME-130

- manifest, runtime contract, generated TypeScript and generated Go inventories
  must contain the same 38 connector IDs;
- operational capabilities must be manifest subsets and unsupported
  connector/entity/direction requests must fail closed at API and worker
  boundaries;
- all 11 ready connectors resolve product readers, while only OpenCart and
  WooCommerce admit outbound product synchronization; PrestaShop admits
  outbound prices and inventory through `torgnexa.commerce-sync.v1`;
- full Go test/vet, contracts, architecture, SDK and frontend production-build
  gates must pass before the runtime images are recreated.

## Phase 16 — Planned connector production composition

`131`

Task 131 activates CBR FX as the first post-Task-130 planned connector. The
existing FX SDK adapter, immutable PostgreSQL repository and Finance page are
now composed with an immediate/six-hour worker refresh through the official
dated Bank of Russia XML endpoint. Runtime inventory becomes 11 generic product
integrations, seven working separate-surface providers and 20 planned entries.

### Gate RUNTIME-131

- the worker must obtain CBR data only through the common host-owned HTTPS and
  public-address egress boundary;
- immutable facts and resolution evidence must remain the only authority;
- source outage must not terminate unrelated worker components, while stale
  data still fails closed after the reviewed freshness window;
- generation, Go test/vet, contracts, architecture, frontend build and live
  worker persistence checks must pass before Task 131 is complete.

## Phase 17 — Telegram Social production composition

`132`

Task 132 composes the existing Telegram SDK adapter and canonical Social Core
through authenticated channel/publication APIs, a dedicated `/social` product
surface and leased worker delivery. The executable subset is deliberately
`social.post.text` only. Append-only remote receipts allow a crashed
`publishing` lease to finalize safely; absence of a receipt becomes
`write_outcome_unknown` and is never auto-sent again. Runtime inventory becomes
11 generic product integrations, eight working separate-surface providers and
19 planned entries.

### Gate RUNTIME-132

- bot tokens remain callback-scoped in SecretProvider and `chat_id` is strict
  negative non-secret configuration;
- API, Core and worker remain provider-neutral; Telegram branching is confined
  to built-in composition;
- scheduled/ready/publishing transitions retain audit/outbox evidence and
  worker lease recovery cannot duplicate an ambiguous remote write;
- migration 17, OpenAPI/SDK generation, Go test/vet, contracts, architecture,
  frontend build and live Telegram qualification must pass before completion.

## Phase 18 — MAX Social production composition

`133`

Task 133 composes the existing MAX adapter through the same provider-neutral
Social API, leased worker and append-only receipt recovery introduced by Task
132. At that point only `social.post.text` was executable: the provider ceiling
was 4000 Unicode code points and the host permitted only exact account/channel
health reads plus `POST /messages?chat_id=...`. Task 175 extends the MAX worker
route with released image, gallery and supported-video publication. Runtime
inventory becomes 11 generic product integrations, nine working
separate-surface providers and 18 planned entries.

### Gate RUNTIME-133

- bot tokens remain callback-scoped and `chat_id` is strict non-zero non-secret
  configuration;
- API/Core/worker remain provider-neutral and MAX protocol branching stays in
  built-in composition;
- Task 175 closes the released-media upload bridge; buttons, status and webhooks
  remain fail-closed despite their SDK presence;
- Task-132 ambiguous-write recovery cannot duplicate a remote message;
- generation, Go test/vet, contracts, architecture, frontend build and live MAX
  qualification must pass before a deployment claims complete production proof.

## Phase 19 — Host-owned OAuth refresh runtime

`134`

Task 134 fixes the generic connector credential boundary before another OAuth
provider is admitted. The encrypted authorization-code bundle remains host-only;
provider adapters receive only a current access token through the frozen SDK-v1
callback. Expiring bundles refresh lazily, rotate under the same opaque reference
and are serialized across API/worker by a tenant/reference PostgreSQL advisory
lock with a post-lock reread. Client-credentials grants exchange without browser
interaction. The catalog remains 11 generic, nine separate and 18 planned.

### Gate RUNTIME-134

- refresh/client material never crosses into provider adapters, logs, events,
  audit, normal tables or API responses;
- concurrent expired-token consumers perform one remote refresh and one
  immutable secret-version rotation;
- missing/rejected refresh tokens produce bounded reauthorization health while
  temporary endpoint/rotation failures remain distinguishable;
- Connector SDK v1, OpenAPI, events, migrations and readiness counts do not
  change;
- Go test/vet, contracts, architecture, migration, frontend and rebuilt
  API/worker health gates passed for the Task-134 boundary; Task 135 is now
  repository-complete for its admitted VK text-publication slice.

## Phase 20 — Bitrix24 CRM production composition

`139`

Task 139 admits the already qualified Bitrix24 adapter on a dedicated `crm`
surface. The host-owned OAuth runtime supplies current access tokens, while a
strict tenant-scoped `portal_host` config reaches the common pinned HTTPS
transport. Entity and product-row CRM capabilities are executable through the
provider-neutral built-in registry; generic product synchronization remains
unavailable because the worker has no CRM-to-product bridge. Runtime inventory
is now 11 generic product integrations, 13 working separate-surface providers
and 15 planned entries.

### Gate RUNTIME-139

- Bitrix24 account creation, OAuth/refresh, portal configuration and health
  checks must use the normal tenant-scoped connector-account boundary;
- the four advertised CRM capabilities must resolve through the built-in
  registry, while generic `products` sync remains fail-closed;
- access/refresh material stays in SecretProvider and portal hosts use the
  common public-HTTPS/SSRF policy;
- runtime-support generation, Go test/vet, contracts, architecture, frontend
  tests/build and package-index checks must pass before release qualification;
- live CRM qualification requires a dedicated non-production Bitrix24 portal
  and OAuth application; repository tests cannot manufacture that external
  fact.

## Phase 21 — Claude AI provider production composition

`141`

Task 141 admits Claude (Anthropic) on the existing tenant-scoped AI-provider
settings surface. The connector uses Anthropic's Messages API through the
host-owned DNS-pinned HTTPS transport, sends one bounded non-streaming text
completion, and keeps API-key material inside the SecretProvider callback.
Runtime inventory is now 11 generic product integrations, 14 working
separate-surface providers and 15 planned entries. The generic product worker
and its `products` bridge remain unchanged.

### Gate RUNTIME-141

- Claude account creation, credential enrollment, analysis and health checks
  must reuse the existing AI-provider API, governance and audit boundaries;
- the provider package must have no direct network or secret-store imports;
  all egress must use the built-in host transport and only the bounded text
  capability may be advertised;
- migration `000020` and the OpenAPI/generated SDK/catalog projections must be
  synchronized, with no secret or tenant text added to durable records;
- runtime-support generation, Go test/vet, contracts, migration, architecture,
  frontend tests/build and package-index checks must pass before release
  qualification;
- live Claude qualification requires a dedicated non-production Anthropic API
  key and retained health/completion evidence; repository fixtures cannot
  manufacture that external fact.

## Phase 22 — Logistics delivery verification surface

`142`, `143`

Tasks 142 and 143 admit 5Post and ПЭК to the separate «Доставка» surface. The
catalog now permits tenant account creation, encrypted credential enrollment
and an authenticated provider health check for both carriers. Their SDK
adapters include deterministic rates/shipment/tracking/pickup tests and
conformance candidates, while generic product synchronization and live
shipment writes stay fail-closed until current carrier fixtures and
non-production credentials are qualified.

### Gate RUNTIME-142-143

- 5Post uses the partner API-key token probe; ПЭК uses the official personal
  cabinet Basic login/access-key probe;
- ПЭК additionally exposes bounded read-only `pickup.points.read` and
  `logistics.rates.read` routes for its branch/warehouse directory and
  calculator, plus one-code cancellation of a pre-registration through
  `/order/cancellation/`; shipment creation, formed-cargo cancellation and
  other write routes remain closed;
- no carrier credential or recipient data is logged, persisted in plaintext or
  copied into events;
- runtime support is `separate_surface/logistics`, never `planned`, while no
  unqualified write capability is enabled;
- generated catalogs, architecture reviews, SDK tests, contracts and frontend
  build remain synchronized before deployment.

## Phase 23 — CDEK and Деловые Линии delivery verification

`145`

Task 145 moves CDEK out of `planned` and adds Деловые Линии to the separate
«Доставка» surface. Both connectors support encrypted account enrollment and a
bounded authenticated health probe: CDEK uses OAuth client credentials plus a
city-directory read, while Деловые Линии uses appkey/PAT session login. CDEK
also has a bounded read-only ПВЗ route (`pickup.points.read`), and Деловые Линии
has the same bounded terminal/PUDO read route plus bounded rate and order-status
history reads (`logistics.rates.read`, `logistics.track.read`). CDEK, ПЭК and
Деловые Линии admit bounded read-only rate previews (`logistics.rates.read`)
with fixed-decimal money normalization, while CDEK, ПЭК and Деловые Линии also
admit a bounded `logistics.track.read` status lookup. Деловые Линии дополнительно
admit только address-to-address shipment-create с явной конфигурацией контрагента,
характера груза, даты и окна передачи; PDF-форма накладной доступна по
`docUID`; only the bounded CDEK refusal/client-return variants are admitted;
terminal/create variants and other return variants are not. Runtime inventory
keeps logistics as a separate surface and admits only these exact CDEK
rate/tracking/PVZ/create/cancel/refusal/client-return/label and
verified `ORDER_STATUS` webhook, ПЭК rate/tracking/PVZ/cancel-pre-registration/label, Деловые Линии
rate/tracking/PVZ/create/label and Почта России
rate/tracking/PVZ/label/single-backlog-create/single-new-order-cancel routes.

### Gate RUNTIME-145

- CDEK and Деловые Линии credentials remain callback-scoped in SecretProvider;
  access/session tokens are discarded after the health probe;
- both providers resolve through the reviewed built-in host transport and are
  represented as `separate_surface/logistics`, never as generic product sync;
- generated runtime support/catalog, deterministic connector tests, contracts,
  architecture review, package index and frontend build must pass;
- live carrier qualification still requires tenant-scoped non-production
  credentials and retained provider evidence before any operational write is
  enabled.

## Phase 24 — Ozon Pay and Ozon Доставка surfaces

`147`

Task 147 adds Ozon Pay to the separate Payments surface and Ozon Доставка to
the separate Delivery surface. Both use encrypted Seller API credentials and
bounded host-mediated access probes. The cards are configurable and visible in
the frontend, but payment mutations and delivery rates/shipments/labels/
tracking remain closed until current Ozon merchant contracts are qualified.
The runtime inventory is 13 generic integrations, 20 separate-surface
providers and 15 planned entries.

### Gate RUNTIME-147

- Seller API keys remain callback-scoped and never appear in logs, events or
  normalized health evidence;
- `ozon-pay` and `ozon-delivery` are registered as separate surfaces with zero
  operational capabilities, so no payment or shipment route can be enabled;
- generated catalogs, architecture policy/reviews, deterministic transport
  tests, contracts, docs and frontend build are synchronized;
- enabling payment/delivery operations still requires a dedicated non-
  production Ozon qualification and an updated capability audit.

## Phase 25 — Local AI provider runtime

`149–150`

Tasks 149 and 150 admit Ollama, LM Studio and Open WebUI on the dedicated
AI-provider
surface. All three use the existing governed `ai.completion.generate` route
and OpenAI-compatible non-streaming chat shape. The runtime adds a separate
host-mediated local transport that resolves and pins only approved private or
loopback destinations, disables proxies and redirects, and bounds request and
response bodies. The small production Compose profile remains model-service
agnostic; operators connect an already-running local server.

### Gate RUNTIME-149

- the three manifests, runtime-support contract, OpenAPI enum, migration
  `000021`, generated catalogs and architecture reviews agree;
- local HTTP is accepted only for the explicit Ollama/LM Studio/Open WebUI,
  loopback and Docker host-gateway names; hosted providers remain HTTPS-only;
- credentials stay callback-scoped, prompts remain governed and audited, and
  no product/order synchronization, streaming or tool capability is claimed;
- connector/transport/API/frontend/contract/migration tests and the production
  Compose rebuild pass before release qualification;
- live model availability still requires an operator-provided local service and
  model; TORGNEXA does not download weights or auto-deploy model servers.

## Phase 25.5 — Medusa v2 storefront qualification

`146`

Task 146 adds the Medusa v2 Connector SDK implementation and catalog card. The
canonical deterministic report is 13/13, while a real Docker/live store is a
separate gate. `scripts/medusa-smoke.sh` exercises the Admin REST API with a
secret API key and is documented in
`docs/connectors/medusa/docker-live-qualification.md`.

### Gate RUNTIME-146

- a non-production Medusa v2 DTC Starter/Compose project, secret API key and
  synthetic SKU are required;
- reads cover catalog, variants/prices, inventory locations and optional
  orders/returns; writes are explicit and restore original values;
- the repository DTC Starter Docker smoke passed on 2026-08-29 with
  read-after-write reconciliation and automatic restoration; an external
  staging endpoint remains a separate live gate, and product creation/webhook
  receipt stay fail-closed.

## Phase 26 — 1С-Битрикс storefront connector

`152`

Task 152 adds 1С-Битрикс as a distinct self-hosted internet-store connector,
separate from the existing Bitrix24 CRM surface. The official REST-module
webhook bridge admits product, price, inventory and order reads, plus
idempotent product/price/inventory writes and order-status writes through the
tenant-scoped commerce-sync route. The card exposes the required
information-block ID and keeps webhook credentials encrypted.

### Gate RUNTIME-152

- the REST module and webhook prerequisite is explicit in the connector docs
  and UI setup guidance;
- `catalog.product.list/get/add/update` calls use the host-mediated transport,
  bounded responses, exact information-block filtering and read-after-write
  verification;
- runtime support, generated catalogs, architecture policy/review, frontend
  presentation, task docs and conformance evidence are synchronized;
- offers/variants, arbitrary custom-property mappings and webhook receipt
  remain fail-closed; inventory writes still require an explicit warehouse
  mapping and order status writes require the canonical status map;
- Go test/vet, contracts, architecture, frontend tests/build and package-index
  checks pass before release qualification;
- live qualification still requires a dedicated non-production 1С-Битрикс site,
  enabled REST module and a scoped webhook.

## Phase 26.5 — Magento / Adobe Commerce storefront qualification

`151`

Task 151 adds the Magento / Adobe Commerce Connector SDK implementation and
catalog card. The canonical deterministic report is 13/13, but this does not
prove a merchant installation. The credentialed REST gate is
`scripts/magento-smoke.sh`, with Docker/project prerequisites and cleanup
documented in `docs/connectors/magento/docker-live-qualification.md`.

### Gate RUNTIME-151

- a non-production Magento/Adobe Commerce project, activated Integration token
  and synthetic SKU are required;
- read checks cover the products, legacy stockItems and optional order/
  creditmemo endpoints; writes are explicit and restore original values;
- until the smoke passes on a real store, Magento remains repository-qualified,
  not live-qualified, and unsupported product-create/SKU-rename/webhook
  operations remain fail-closed.

## Phase 27 — CS-Cart storefront connector

`153`

Task 153 adds CS-Cart as a distinct self-hosted internet-store connector. The
official API 2.0 Basic Auth surface admits product catalog reads and
idempotent creates/updates with SKU lookup and read-after-write reconciliation,
bounded base-price and single-storefront inventory reads, and bounded order
list/detail reads; price/inventory writes use product PUT with
read-after-write; standard order-status writes use order PUT with
read-after-write; order creation/deletion, custom status codes and webhooks
remain unavailable.

### Gate RUNTIME-153

- API access activation, administrator e-mail/API key credentials and
  `store_host`/`base_path`/`store_currency` runtime configuration are explicit;
- product list/get/create/update calls use the host-mediated HTTPS transport
  with bounded responses and cursor pagination;
- runtime support, generated catalogs, architecture policy/review, frontend
  presentation, task docs and conformance evidence are synchronized;
- order creation/deletion, custom status codes and webhook receipt remain
  fail-closed;
- option-combination order lines and unknown remote status codes remain
  fail-closed rather than being projected ambiguously;
- Go tests/vet, contracts, frontend tests/build and package-index checks pass;
- live qualification still requires a non-production CS-Cart store with API
  access enabled for the administrator. The credentialed API 2.0 smoke is
  documented in `docs/connectors/cs-cart/docker-live-qualification.md` and
  implemented by `scripts/cscart-smoke.sh`; until it passes, the provider is
  repository-qualified only (SDK 13/13), not live-qualified.

## Phase 27.25 — Shopify storefront qualification

`144`

Task 144 admits Shopify through the host-owned per-tenant OAuth runtime. Shopify
has no official self-hosted Docker store, so the reproducible local gate is a
stateful Admin REST protocol double rather than a fake merchant deployment.
The double passed on 2026-08-29 against the pinned Admin REST API `2026-07`:
auth rejection, health/version, catalog, locations, variant/inventory mapping,
orders/refunds, product/price/inventory writes, read-after-write and cleanup.

### Gate RUNTIME-144

- run `docker-compose.shopify-test.yml` with `scripts/shopify-smoke.sh` for
  protocol qualification; it never contacts Shopify or accepts production
  credentials;
- a real Shopify Dev Store, installed app token, required scopes and a
  synthetic SKU are still required for external qualification, using the same
  smoke script over HTTPS;
- webhook receipt remains fail-closed, and order
  cancel/close/reopen writes are excluded from the smoke because they do not
  have a safe complete rollback in the generic test contract;
- status is tracked in `docs/connectors/shopify/live-qualification-status.json`.

## Phase 27.4 — Shopware storefront qualification

`148`

Task 148 adds Shopware 6 as a self-hosted storefront connector. The SDK
conformance report is 13/13 PASS. On 2026-08-29 a disposable Shopware 6.7
Docker store passed credentialed Admin API smoke for OAuth, catalog/detail,
currency/price, stock, orders, refunds, product/price/stock writes,
read-after-write and cleanup. The connector was corrected to accept both the
current JSON:API response (`data.attributes`, `meta.total`) and flat DAL
responses; refunds use the public hyphenated entity route.

### Gate RUNTIME-148

- run `docker-compose.shopware-test.yml` with `scripts/shopware-smoke.sh` and a
  temporary Integration credential; the disposable all-in-one Dockware image
  is community-supported and must not receive production data;
- product create and incoming webhooks remain fail-closed; order cancellation
  is not part of the automatic smoke because it is irreversible;
- external merchant qualification remains separate and requires an HTTPS
  endpoint, scoped Integration credential and synthetic SKU; status is tracked
  in `docs/connectors/shopware/live-qualification-status.json`.

## Phase 27.5 — Saleor storefront qualification

`154`

Task 154 adds the self-hosted Saleor GraphQL connector and its qualification
split. The canonical Connector SDK report is 13/13 PASS. A disposable official
Saleor Platform Docker stack was then credential-smoke-tested on 2026-08-29:
catalog/detail reads, channel/warehouse resolution, product/name/publication,
price and stock writes, read-after-write reconciliation and cleanup all passed
for SKU `111223580`.

### Gate RUNTIME-154

- the reproducible stack is
  `docker-compose.saleor-test.yml` with `scripts/saleor-smoke.sh` and the
  procedure in `docs/connectors/saleor/docker-live-qualification.md`;
- product creation remains fail-closed because the current Connector SDK
  contract cannot carry Saleor's required product type; current no-secret
  webhook receipt is admitted through detached RS256/JWKS verification and
  the public commerce webhook route, while the deprecated HMAC `secretKey`
  variant remains closed;
- external merchant staging remains a separate gate requiring an HTTPS
  endpoint, scoped App token and synthetic channel/warehouse/SKU. The status is
  tracked in `docs/connectors/saleor/live-qualification-status.json`.

## Phase 28 — «Почта России» logistics connector

`155`

Task 155 adds «Почта России» to the separate «Доставка» surface. The official
Otpravka application token and user authorization key are stored encrypted and
checked through a fixed HTTPS settings probe. A bounded read-only
`pickup.points.read` route searches offices by city and loads each returned
office card by postal index. A separate read-only `logistics.rates.read` route
uses the official tariff calculator with postal indexes and total parcel weight;
`logistics.track.read` uses the separate official SOAP `getOperationHistory`
service for one domestic or S10 barcode. `logistics.label.read` requests the
official PDF order form for a numeric backlog order ID and returns only an
opaque content-addressed reference. `logistics.shipment.create` creates one
strictly mapped order in the backlog, while `logistics.shipment.cancel` deletes
one new order only after the response confirms the exact ID. Batch formation,
hand-off and return operations remain closed until current test-account
fixtures qualify the REST/API contracts.

### Gate RUNTIME-155

- the Delivery card, manifest, runtime-support contract, policy/review and
  generated catalogs agree on `separate_surface/logistics` with only bounded
  `pickup.points.read`, `logistics.rates.read`, `logistics.label.read`,
  `logistics.track.read`, single-backlog `logistics.shipment.create` and
  exact-single-order `logistics.shipment.cancel`
  admitted;
- credentials stay callback-scoped, strict JSON decoding rejects unknown fields,
  and the host sends only the documented authentication headers to the fixed
  `otpravka-api.pochta.ru` host; the separate public tariff host receives no
  account credentials;
- deterministic connector, transport, conformance, contract and frontend
  checks pass without production credentials or network access;
- shipments and returns cannot be enabled until
  non-production provider qualification is retained.

## Phase 29 — Категорийные health-check поверхности

`156`

Task 156 closes the remaining 14 planned catalog entries as explicit
category-specific `separate_surface` records: Auto.ru, Avito and CIAN are in
«Объявления и вертикали»; Instagram, Odnoklassniki, Rutube, Threads, VK and
YouTube are in «Социальные сети»; Diadoc and Saby EDO are in «ЭДО»; Chestny
ZNAK, EGAIS and VetIS/Mercury are in «Госсистемы». A new `health_only` contract
flag enables tenant-scoped credentials and authenticated, bounded host probes
without inventing domain operations.

### Gate RUNTIME-156

- generated support/catalog parity reports 61 providers: 18 `ready`, 43
  `separate_surface`, 0 `planned`;
- the UI groups cards by category and clearly labels health-only accounts;
- API enablement rejects non-empty capabilities for these rows and allows only
  the health-check account path;
- arbitrary hosts, non-HTTPS probes and unknown credential placeholders fail
  closed;
- contract generation, Go tests/vet, frontend tests/build and documentation
  checks pass;
- domain publication, synchronization, EDO and government writes remain
  qualification-gated and are not represented as production capabilities.

## Phase 30 — PrestaShop commerce sync runtime route

`160`

Task 160 closes the gap between the qualified PrestaShop Webservice price and
StockAvailable writes and the production worker. A dedicated
`torgnexa.commerce-sync.v1` consumer routes canonical price/inventory events
through enabled outbound policies, tenant-scoped offer mappings and the
existing connector receipt/retry machinery. Only regular prices and discrete
stock quantities are admitted; reads, fractional units and other entity writes
remain outside this route.

### Gate RUNTIME-160

- PrestaShop is the only runtime-support row advertising outbound `prices` and
  `inventory` sync, and generated Go/TypeScript catalogs agree;
- account capability snapshots, offer mappings and deterministic receipts are
  checked before/after every remote write;
- retryable provider failures use Kafka retry topics while malformed,
  unmapped and non-retryable failures use the DLQ;
- route tests, Go test/vet, contract and architecture checks pass, with the
  Docker Webservice smoke retained as the provider-level qualification.

## Phase 31 — Canonical product event runtime route

`161`

Task 161 closes the outbound catalog gap: `commerce-sync` no longer silently
ignores `commerce.catalog.product_changed.v1`. For every connector admitted by
the generated runtime-support contract, the worker loads the current
tenant-scoped product, resolves or safely creates the `product` mapping, maps
the lifecycle status at the built-in provider boundary, and records the
validated applied/duplicate receipt. Price and inventory events retain their
existing offer-mapping and PrestaShop-only route.

### Gate RUNTIME-161

- product event payload validation, canonical snapshot loading and policy,
  account-capability and runtime admission remain fail-closed;
- provider-native status translation matches each admitted ProductWriter
  contract, without provider branches in the worker route;
- product creates persist mappings only after a validated remote receipt and
  retry safely after a crash between remote effect and local commit;
- `offer_changed.v1` remains ignored and price/inventory mapping behavior is
  unchanged;
- Go tests/vet, contract and architecture checks pass before release
  qualification.

## Phase 32 — Authorized Community browser E2E

`162`

Task 162 closes the local browser-verification gap. The Community workflow now
reconciles a synthetic Keycloak user and workspace membership, then uses a
clean Chrome profile to complete the real authorization-code flow and exercise
the catalog, product-card image view, orders and order thumbnails. The runner
uses only Node built-ins and Chrome DevTools Protocol, so the frontend lockfile
and JavaScript supply-chain graph remain unchanged.

### Gate RUNTIME-162

- `make community-e2e` bootstraps the local demo identity before opening the
  browser;
- anonymous access cannot satisfy the test: the browser must complete the
  Keycloak login and render the authenticated shell;
- catalog rows, a product-card main image, the image tab and order list/detail
  thumbnails are checked as loaded DOM media;
- order actions remain visible for the seeded pending order; after the
  idempotent demo setup, the browser assertions do not mutate demo state;
- browser profiles, tokens, cookies and tenant selectors are not persisted;
- repository frontend checks remain independent from the runtime E2E.

## Phase 33 — Workflow automation builder

`163`

Task 163 is repository-complete as a provider-neutral automation builder on top
of the existing EventBus/Transactional Outbox/Inbox, PostgreSQL scheduler,
worker, approval and connector-port boundaries. All ten subtasks are covered:

1. `163.1` ADR, scope and typed action catalog;
2. `163.2` canonical workflow model and immutable version lifecycle;
3. `163.3` Draft 2020-12 DSL schema, validation and deterministic compiler;
4. `163.4` tenant-scoped PostgreSQL persistence, RLS and retention;
5. `163.5` EventBus triggers, durable schedules, deduplication and leases;
6. `163.6` execution state machine, conditions, retries and approvals;
7. `163.7` typed safe action adapters and notification/reconciliation/dry-run
   vertical slice;
8. `163.8` REST/OpenAPI contracts and operator builder/run UI;
9. `163.9` quotas, observability and operator recovery;
10. `163.10` contract, security, load/chaos, Compose E2E and documentation
    qualification.

### Gate RUNTIME-163 — repository-complete

- workflow definitions are immutable after publish and every run is
  tenant-scoped, idempotent and resumable;
- no arbitrary code, SQL, shell, browser automation, unbounded loops or direct
  provider/secret access can be represented by the DSL;
- sensitive and legally-significant actions reuse Task-017 approval and all
  side effects use existing capability, policy, audit and outbox boundaries;
- duplicate events, worker crashes, lease loss, retryable/permanent failures,
  approval expiry and connector outages have deterministic outcomes;
- per-workspace limits prevent fan-out, memory, connection-pool and retry
  storms on the small-VPS Compose profile;
- API/UI expose only validated actions and current runtime capabilities;
- full Go, contract, architecture, migration, frontend, conformance,
  performance and repository qualification checks pass. Live deployment
  capacity, target-topology chaos and live-provider evidence remain separate
  release gates.

## Phase 34 — Возвраты, отмены и refunds

`164`

Task 164 is planned as a provider-neutral operational contour for order
cancellation, partial/full returns and payment refunds. It is explicitly
separate from the immutable order snapshot and from payment, fulfillment,
inventory, fiscal and settlement facts, while coordinating them through typed
ports. The implementation is split into twelve subtasks:

1. `164.1` ADR, terminology, lifecycle scope and policy/approval matrix;
2. `164.2` canonical cancellation/return/refund contracts, allocations and
   transition/invariant validators;
3. `164.3` order-payment-fulfillment-inventory orchestration and compensation
   contract;
4. `164.4` tenant-scoped PostgreSQL schema, FORCE RLS, idempotency and
   append-only evidence;
5. `164.5` canonical events, Outbox/Inbox and verified provider webhooks;
6. `164.6` durable cancellation worker with leases, retries and unknown-outcome
   handling;
7. `164.7` return authorization, logistics, receipt/inspection and WMS
   disposition;
8. `164.8` refund orchestration with fiscalization, settlement and bounded
   reconciliation;
9. `164.9` REST/OpenAPI and order/operator return/refund UI;
10. `164.10` per-connector capability/runtime/conformance qualification;
11. `164.11` security, metrics, quotas, alerts and recovery runbook;
12. `164.12` unit/contract/RLS/API/worker tests, Docker/Compose E2E,
    load/chaos, screenshots, docs and retained evidence.

### Gate RUNTIME-164

- cancellation, return and refund lifecycles are separate, versioned and
  tenant-scoped; immutable order/payment/inventory/fiscal facts are never
  silently rewritten;
- full and partial line returns, multiple refunds and tax/shipping allocations
  enforce exact money/quantity and no-over-refund/no-over-return invariants;
- all external effects use capability ports, policy/approval, idempotency,
  timeout and retry classification; accepted-but-unknown outcomes reconcile
  without blind re-issue;
- order, shipment, WMS, fiscal and settlement effects are independently
  evidenced and replay-safe through Outbox/Inbox/webhook deduplication;
- only connectors with current runtime route plus conformance and Docker/live
  evidence expose cancellation/return/refund capabilities; others fail closed;
- per-workspace limits keep refund/return bursts, webhook storms, memory,
  connection pools and Kafka lag bounded on the small-VPS Compose topology;
- Go, contract, architecture, migration, frontend, conformance, performance,
  Compose E2E and documentation checks pass before production admission.

## Phase 40 — Маркировка, агрегация и УПД

`171`

Task 171 is repository-complete as a provider-neutral marking execution
contour. The fifteen subtasks cover the operation matrix and ADR, safe
fingerprint-only code storage, typed marking SDK writes with dry-run and
unknown outcomes, code request/reservation, print and scan, unit/kit/box/
pallet aggregation, circulation/transfer, versioned UPD 5.03 and EDO state,
full orchestration, reconciliation drifts, operator API/UI and synthetic
Docker qualification. Live Chestny ZNAK, Diadoc, Saby, KKT/OFD and
marketplace credentials and legal qualification remain external release
gates. See `tasks/issues/171-marking-execution-and-upd.md`.

### Gate RUNTIME-171

- no raw marking code is stored in PostgreSQL, events, audit metadata, logs,
  normal API responses or SDK result types; only fingerprints and expiring
  artifact references cross durable boundaries;
- all remote marking writes are typed, capability/approval gated,
  idempotent, dry-run aware and preserve `unknown` after an ambiguous result;
- package graph, one-use printing, duplicate/wrong/overflow scans, UPD lines,
  signing/MChD, EDO statuses and reconciliation drifts are explicit;
- migration 000037 is backup-gated, tenant-scoped, FORCE RLS and protects
  scans, observations and drifts with append-only constraints;
- API/OpenAPI/SDK/UI, unit/contract/architecture/migration/conformance and
  synthetic Docker checks pass before any external connector admission.

## Phase 40 — Рабочее место WMS-оператора и marketplace fulfillment

`170`

Task 170 introduces the first durable provider-neutral fulfillment execution
slice. It connects canonical order items and existing fulfillment allocations
to tenant-scoped WMS tasks, with idempotent scanner commands, immutable task
history, PostgreSQL RLS and Transactional Outbox evidence. The implementation
is complete for the foundation and selected follow-up stages:

1. `170.1` ADR, scope, state machine and policy matrix;
2. `170.2` durable PostgreSQL task/event model and repository;
3. `170.3` permission-aware WMS REST/OpenAPI and generated SDK;
4. `170.4` atomic order → allocation → pick-task orchestration.
5. `170.5` task context, locations and scan traceability;
6. `170.6` standalone receiving, put-away and cycle-count execution;
7. `170.7` bounded work batches and local pack handoff;
8. `170.9` operator workspace UI;
9. `170.12` qualification checks, documentation and evidence summary.

### Gate RUNTIME-170

- every task and event is tenant-scoped, versioned, idempotent and auditable;
- exact quantities, allocation ownership and ATP remain authoritative in
  PostgreSQL; task execution never silently rewrites order snapshots;
- claim/start/scan/complete/exception/cancel commands are replay-safe and
  fail closed on terminal state, version conflict, inactive warehouse or
  insufficient stock;
- the public API exposes only bounded provider-neutral task contracts and all
  generated SDKs remain in parity with OpenAPI;
- marketplace order writes, labels, ChZ, external shipment/status writes and
  production qualification remain explicit follow-up tasks; 170.9 covers only
  the internal operator workspace.

## Phase 36 — Центр качества публикации товаров

`166`

Task 166 is repository-complete as a provider-neutral preflight and operational quality
center for Product/Offer publication. It evaluates the exact local snapshot
against a connector-account publication profile, current capability, mapping,
media release and Task-082 compliance evidence. It never becomes a second PIM,
publication state machine or stock/compliance source of truth. The
implementation is split into thirteen subtasks:

1. `166.1` ADR, scope, severity, score and rule governance (complete);
2. `166.2` canonical quality model, immutable snapshots and gate receipts (complete);
3. `166.3` versioned declarative publication-profile/rule schema (complete);
4. `166.4` bounded catalog/PIM/price/stock/media/compliance snapshot contract (complete);
5. `166.5` deterministic rule engine, score and remediation hints (complete);
6. `166.6` `commerce-sync` pre-publication gate and compliance-guard composition (complete);
7. `166.7` EventBus/scheduler integration boundary and quality worker contract (complete);
8. `166.8` PostgreSQL/RLS, lineage, retention and bounded indexes (complete);
9. `166.9` REST/OpenAPI, permissions and Product Quality Center UI (complete);
10. `166.10` typed remediation proposals and Task-017 approval boundary (complete);
11. `166.11` connector profile/remote-preflight qualification boundary (complete);
12. `166.12` security, observability, quotas, alerts and recovery runbook (complete);
13. `166.13` unit/property/contract/RLS checks, frontend/SDK build, Docker
    instructions, documentation and retained synthetic evidence contract (complete;
    live connector qualification remains release-topology specific).

### Gate RUNTIME-166

- quality decisions are target-specific, tenant-scoped, versioned and bound to
  exact product/offer/profile/rule/capability/compliance snapshots;
- `ready`/`ready_with_warnings`, `blocked`, `approval_required`, `stale`,
  `unsupported` and `unknown` are distinct; a numeric score never overrides a
  hard blocker or missing authority;
- rules cover canonical identity/content, PIM category/attributes,
  localization, media release/security, price/stock semantics, mapping,
  capability freshness and Task-082 compliance without duplicating its
  evaluator;
- `commerce-sync` and every future product writer make no remote call without
  an exact valid quality receipt and still pass the final compliance guard;
- remediation is typed/idempotent/optimistic, sensitive changes use Task-017
  approval, and AI/MCP/n8n cannot auto-edit or bypass policy;
- duplicate/out-of-order events, stale snapshots, profile changes, worker
  crashes, remote validation rejection and connector outages produce no false
  `published` state or duplicate side effect;
- only connectors with current runtime route, profile and conformance plus
  Docker/live evidence expose publication readiness; health-only/SDK-only
  targets remain fail-closed;
- per-workspace limits bound rule count, catalog scans, media/attribute size,
  queue fan-out, remote calls, memory and DB/Kafka load on small-VPS Compose;
- Go, contract, architecture, migration, frontend, conformance, performance,
  Compose E2E and documentation checks pass before production admission.

## Phase 37 — Юнит-экономика по каналам

`167`

Task 167 is repository-complete as a provider-neutral factual unit-economics contour by
channel, store, order and Offer/SKU. It extends the existing `profitability-v1`
what-if scenario and reporting foundation with explicit accounting bases,
immutable calculation runs, historical COGS, settlement/payment deduplication,
advertising and return allocations, sourced FX and completeness evidence. The
ledger and canonical commerce facts remain authoritative; ClickHouse is only a
rebuildable projection. The implementation is delivered as the eighteen-subtask contract:

1. `167.1` ADR, terminology, accounting scope, bases and metric policy;
2. `167.2` tenant-scoped channel identity, mapping and attribution resolution;
3. `167.3` exact metric/sign/quality contracts and compatibility versions;
4. `167.4` normalized Order/Settlement/Payment/Ads/Return/COGS/FX fact inputs;
5. `167.5` historical COGS snapshot and inventory valuation policy;
6. `167.6` deterministic shared-income/expense allocation with conservation;
7. `167.7` settlement/payment deduplication and reconciliation evidence;
8. `167.8` advertising, promotion and attribution-window accounting;
9. `167.9` cancellation, return, refund, shipping and tax treatment;
10. `167.10` cross-currency conversion, rounding and FX evidence;
11. `167.11` pure calculation engine and immutable versioned runs;
12. `167.12` ClickHouse schema, projections, replay/backfill and freshness;
13. `167.13` PostgreSQL metadata, RLS, lineage, retention and migration;
14. `167.14` Outbox/Inbox events, scheduler, worker, leases and recovery;
15. `167.15` REST/OpenAPI, permissions, snapshot filters and exports;
16. `167.16` operator UI for channel comparison and explainable drill-down;
17. `167.17` security, privacy, observability, quotas and incident runbooks;
18. `167.18` unit/property/contract/RLS/Compose/load tests, screenshots, docs
    and retained release evidence.

### Gate RUNTIME-167

- every row is tenant-scoped and bound to a stable `channel_ref`, one explicit
  `order_accrual`/`settlement`/`cash` basis, original/reporting currency,
  formula/allocation/valuation/attribution versions and immutable input digest;
- GMV, net revenue, COGS, fees, logistics, ads, refunds, compensation and
  contribution metrics follow the approved sign/conservation policy; payout is
  never counted as revenue and Order + settlement + Payment cannot double count;
- `complete`, `partial`, `stale`, `unmatched`, `conflict`, `mixed_currency` and
  `unsupported` remain distinct; missing COGS/FX/attribution is visible and is
  never zero-filled;
- returns/cancellations/refunds, adjustment chains and historical COGS are
  replayable without mutating immutable Orders, Payments, Inventory or
  SettlementEntry facts;
- all cross-currency totals use persisted Task-089b conversion evidence and
  final-output rounding only; stale/missing/ambiguous FX fails closed;
- calculation snapshots are immutable, ClickHouse rebuildable, and duplicate,
  late, out-of-order and corrected facts converge to one deterministic run;
- API/OpenAPI/SDK/UI/export expose source, watermark, coverage, quality,
  allocations and explanation for the exact selected run;
- forced RLS, financial permissions, privacy/retention/legal-hold, audit/
  lineage, no-secrets logging, bounded ranges/exports and per-workspace quotas
  pass under concurrent rebuilds;
- worker crash, lease loss, CH outage, DLQ replay, duplicate fee and late
  settlement produce no false profit and no retry storm on small-VPS Compose;
- only synthetic fixtures plus deterministic Compose/runtime evidence qualify
  release; Go, contract, architecture, migration, frontend, conformance,
  performance and documentation checks are green before production.

## Phase 38 — Единый центр состояния интеграций

`168`

Task 168 is complete as a provider-neutral read model and operator triage center
for the complete integration lifecycle. It composes account lifecycle,
credential/configuration class, truthful runtime stage, capability grants,
health/freshness/rate limits, OAuth reauthorization, sync/retry/DLQ,
reconciliation, webhooks, notifications and separate AI/Finance/Delivery/CRM
surfaces without making any of those projections a second source of truth.
The implementation is split into eighteen subtasks:

1. `168.1` ADR, scope, state vocabulary and reducer policy;
2. `168.2` canonical status/evidence contracts and compatibility versioning;
3. `168.3` tenant-scoped bulk source adapters and consistency boundary;
4. `168.4` account lifecycle, credential class and runtime-config state;
5. `168.5` truthful manifest/runtime-support/catalog projection;
6. `168.6` capability grants, executable operation readiness and approvals;
7. `168.7` health, freshness, rate-limit and failure normalization;
8. `168.8` sync, worker, retry/DLQ and reconciliation dimensions;
9. `168.9` OAuth reauthorization and security-posture links;
10. `168.10` notification, issue and operator-action model;
11. `168.11` deterministic aggregate reducer and snapshot consistency;
12. `168.12` PostgreSQL derived metadata, RLS, lineage and retention;
13. `168.13` canonical events, durable worker and metadata-only realtime;
14. `168.14` permission-aware REST/OpenAPI read API and generated SDK;
15. `168.15` responsive Integration State Center UI and deep links;
16. `168.16` idempotent safe operator actions and remediation boundaries;
17. `168.17` security, privacy, SLO, observability, quotas and runbooks;
18. `168.18` unit/property/contract/RLS/Compose/load tests, screenshots, docs
    and retained release evidence.

### Gate RUNTIME-168

- one read model preserves account, runtime, credentials/config class,
  capability, health/freshness/rate-limit, sync/retry/DLQ, reconciliation,
  webhook and separate-surface dimensions; the overall reducer never hides
  secondary issues;
- `healthy` requires fresh authoritative evidence for the selected operation;
  health-only, separate-surface, unsupported, stale, blocked, unknown and
  redacted states never become executable green status;
- every row/snapshot is tenant-scoped, versioned, digest-bound and
  permission-aware with source watermarks, age/TTL, reason code, visibility and
  safe next action; no secret, raw provider error, token or PII is exposed;
- manifest/runtime-support/qualification determines runtime stage while the
  current account grant and host route determine executable capability;
  manifest alone or a successful ping never authorizes a business operation;
- GET performs no remote IO and cannot mutate source state; check, OAuth,
  credential/config, sync, reconciliation, retry and approval actions reuse
  existing idempotent API/worker/connector owners;
- status transitions, notifications, Outbox/Inbox, worker leases, retries,
  SSE invalidation and rebuilds are duplicate/out-of-order/crash/DLQ safe;
  source outage yields partial/stale evidence, never false health;
- API/OpenAPI/SDK/UI preserve tenant permissions, cursor/response limits,
  URL filters, accessible responsive layout and Russian copy distinguishing
  connection check from executable operation;
- RLS, lineage, audit, retention/legal-hold, migration/backup, no-secrets
  logging and small-VPS quotas pass under concurrent refresh/rebuild traffic;
- all unit, contract, architecture, migration, frontend, conformance,
  performance, Compose, screenshot, documentation and release-evidence checks
  are green before production admission.

## Phase 39 — AI-помощник для оператора

`169`

Task 169 is complete as a provider-neutral, grounded operator copilot over the
canonical commerce and operations modules. It is intentionally separate from
the legacy `/settings/ai-providers:analyze` completion call: the server owns
intent, retrieval, data classes, evidence, risk and action limits. The first
release is read/recommendation/preview-first and cannot become an autonomous
administrator. The implementation is split into twenty subtasks:

1. `169.1` ADR, scope, threat model and definition of done;
2. `169.2` canonical session/run/message/answer/evidence/action contracts;
3. `169.3` privacy classes, retention, redaction and transcript policy;
4. `169.4` provider/model registry, routing, egress and cost budgets;
5. `169.5` deterministic intent classifier and question/refusal policy;
6. `169.6` typed source retrieval ports and bounded grounded context builder;
7. `169.7` citations, watermarks, freshness and explainability contract;
8. `169.8` server prompt templates, versioning and injection boundary;
9. `169.9` typed answer composer, claim validation and refusal quality policy;
10. `169.10` typed action catalog and side-effect-free preview compiler;
11. `169.11` Task-017 approval bridge and canonical execution hand-off;
12. `169.12` PostgreSQL forced-RLS persistence, lineage and retention;
13. `169.13` durable run worker, queue, leases, cancellation and streaming;
14. `169.14` EventBus/Outbox/Inbox, audit and Notification Center integration;
15. `169.15` permission-aware REST/OpenAPI and generated SDK;
16. `169.16` Russian operator UI, safe rendering, accessibility and deep links;
17. `169.17` MCP/OpenClaw/n8n boundary, remaining deny-by-default gate;
18. `169.18` security, observability, SLO, quotas, kill switches and runbooks;
19. `169.19` connector/domain readiness matrix and synthetic demo fixtures;
20. `169.20` unit/property/contract/RLS/security/Compose/load tests,
    screenshots, documentation and retained release evidence.

### Gate RUNTIME-169

- answers are tenant/actor-scoped, bounded and grounded in authoritative
  evidence with source refs, watermarks, freshness, visibility and digests;
  `insufficient_data`, `stale`, `partial`, `blocked` and `refused` never become
  confident facts;
- server determines tenant, permissions, intent, data classes, source set,
  provider/model eligibility, risk and limits; model/frontend/external text is
  structurally `UNTRUSTED_TOOL_DATA` and cannot grant authority;
- legacy completion remains compatible, but arbitrary client system prompts or
  data-class claims cannot select the assistant path; provider routing, egress,
  templates and budgets are versioned and governed;
- no raw prompt/response, chain-of-thought, secret, token, private key, raw
  provider payload or unnecessary PII appears in persistence, events, audit,
  logs, URLs, screenshots or exports; retention/legal hold is tested;
- actions are typed/provider-neutral and preview-only by default; writes reuse
  current capability/runtime/quality/compliance/policy checks, Task-017
  approval, expected version, idempotency, audit and canonical workers;
  unknown external outcomes never blind-retry;
- session/run/worker/event/outbox/inbox/notification paths are duplicate,
  out-of-order, crash, lease-loss, cancellation, DLQ and replay safe; source
  outage yields partial/stale evidence, not false completion;
- API/OpenAPI/SDK/UI expose accessible Russian workflow, citations, safe links,
  permission-aware redaction, cursor/ETag/bounds, resume/cancel and clear
  distinction between source fact, AI recommendation, approval and unavailable
  operation;
- MCP/OpenClaw/n8n remain additive and deny-by-default until trusted
  Governor/Auditor composition; no privileged path or credential exposure is
  introduced;
- small-VPS Compose quotas bound context, provider calls, tokens, memory,
  connections, queue/Kafka lag, streams and DB queries; dashboards, alerts,
  kill switches and runbooks enable recovery without stopping commerce;
- all unit/property/contract/RLS/adversarial/frontend/worker/connector/
  Compose/load/chaos/screenshot/documentation checks pass with synthetic data
  before production admission.

## Phase 35 — Прогноз остатков и автопополнение

`165`

Task 165 is repository-complete for its bounded provider-neutral runtime slice.
It turns the
current velocity/lead-time/safety-stock recommendation into a versioned demand
forecast, projected stockout/overstock risk and controlled replenishment plan,
while keeping PostgreSQL/WMS ledger authoritative and ClickHouse analytical
only. The implementation is split into thirteen subtasks:

1. `165.1` ADR, terminology, operating modes and policy/approval matrix;
2. `165.2` canonical planning contracts, forecast points and invariants;
3. `165.3` input ingestion, normalization, watermarks and data-quality gates;
4. `165.4` deterministic forecast baselines, intervals, backtesting and model
   versioning;
5. `165.5` projected stock, stockout/overstock risk and bounded scenarios;
6. `165.6` reorder policy, supplier selection, MOQ/case-pack and budget/capacity
   optimization;
7. `165.7` PostgreSQL persistence, RLS, lineage, retention and indexes;
8. `165.8` EventBus triggers, durable scheduler, coalescing and forecast worker;
9. `165.9` idempotent draft PO and guarded auto-replenishment execution;
10. `165.10` REST/OpenAPI, MCP/n8n boundary and operator forecast UI;
11. `165.11` supplier/source connector capability and runtime qualification;
12. `165.12` security, observability, quotas, kill switch and recovery;
13. `165.13` unit/property/contract/RLS tests, Docker/Compose E2E, load/chaos,
    screenshots, documentation and retained evidence.

### Gate RUNTIME-165

- forecast, projection, recommendation and PO execution are separate,
  tenant-scoped, versioned facts; WMS ledger remains the only stock truth;
- forecast inputs include freshness/quality evidence, exact money/quantity,
  uncertainty and explainable algorithm/model/policy digests;
- full and partial inbound, reservations, quarantine, returns/cancellations,
  lead-time uncertainty, MOQ/case-pack, supplier validity, budget and capacity
  enforce deterministic no-negative/no-over-order invariants;
- `recommendation_only` is the default, `draft_po` is idempotent and
  `auto_submit` is explicit, capped, approval-aware, kill-switchable and
  available only with current connector runtime + conformance evidence;
- PO creation/submission uses the existing procurement state machine and
  Task-017 approval; timeout/unknown supplier outcomes reconcile without blind
  re-issue or duplicate spend;
- forecast/schedule workers use durable PostgreSQL leases, EventBus
  Outbox/Inbox, deduplication, bounded catch-up and per-tenant backpressure;
- API/UI/MCP/n8n expose only current capabilities and never persist secrets,
  raw provider/model payloads or use AI as a privileged executor;
- Go, contract, architecture, migration, frontend, conformance, performance,
  Compose E2E and documentation checks pass before production admission.

## Phase 41 — Yandex Market inventory write

`172`

Task 172 is repository-complete. Yandex Market now admits the provider-neutral
`inventory.write` route through the existing commerce-sync worker. The adapter
maps explicit `partner_warehouses` mode to the documented business v3 stock
update and `campaign_warehouses` mode to the grouped campaign v2 stock update;
both modes validate tenant configuration, numeric warehouse scope, integer
quantity bounds and asynchronous acceptance. The generated runtime/catalog,
registry admission, deterministic provider tests and connector documentation
are synchronized. Product, order-status and other unqualified writes remain
fail-closed; live credentialed staging is a separate release gate.

## Phase 42 — ПЭК bounded shipment create

`173`

Task 173 is repository-complete. ПЭК now admits a bounded
`logistics.shipment.create` path through the existing approval-bound logistics
worker: one Russian self-delivery preregistration (`orderType=0`, `FFS`, cargo
type `3`, service `pek_type_3`) with a configured sender warehouse and no more
than 50 parcels. The host-side adapter validates sender configuration, contacts,
country and dimensions, then accepts only a response with a document identifier
and one numeric cargo code. Provider acceptance remains asynchronous and is not
treated as reconciliation. Formed-cargo cancellation, returns, address
delivery and batch print forms remain fail-closed pending separate qualification.

## Phase 43 — Telegram media publication worker route

`174`

Task 174 is repository-complete. Telegram now composes text, one-photo,
2–10-photo album and one-MP4-video publication through the existing Social
worker. Core media variants are converted to the provider-neutral SDK request;
the worker revalidates Task-088 released-upload evidence and the host sends
bounded multipart requests. URL buttons, edit/delete, inbound webhooks and
arbitrary file types remain fail-closed pending separate application
authorization or provider qualification.

## Phase 44 — MAX media publication worker route

`175`

Task 175 is repository-complete. MAX now composes text, released image/gallery
and supported video publication through the existing Social worker. The host
implements the documented `/uploads` initialization and exact image/video
upload-host allowlist, then sends bounded multipart `data` bodies with the
callback-scoped bot token. Buttons, webhooks, status reads, destructive
mutations and arbitrary files remain fail-closed.

## Phase 45 — Robokassa merchant refund runtime

`176`

Task 176 is repository-complete. Robokassa now admits `payments.refund` through
the official merchant Refund API at `/RefundService/Refund/Create`. The
transport reads the authoritative payment with OpStateExt, requires state
`100` and a non-empty `Info.OpKey`, signs the compact HS256 JWT with Password3,
omits `RefundSum` for a full refund and sends exact RUB decimal minor units for
a partial refund. The asynchronous `requestId` is returned as `accepted`;
malformed credentials, missing OpKey, non-successful payment and ambiguous
network outcomes remain fail-closed.

## Phase 46 — Почта России — возвратная этикетка

`177`

Task 177 is repository-complete. Почта России now supports the explicit
`return_pdf` label format for a domestic/S10 RPO through
`GET /1.0/forms/{rpo}/easy-return-pdf`. The host validates the PDF response and
returns only a content-addressed opaque artifact reference. Regular backlog
forms remain on `format=pdf`; separate return shipments, batch formation and
hand-off remain fail-closed.

## Phase 47 — ПЭК — печатная форма заявки

`178`

Task 178 is repository-complete. ПЭК now supports the explicit `request_pdf`
label format through the official `/api/v1/order/print/` route with `type=big`.
The host validates the bounded base64 response as a PDF and returns only a
content-addressed opaque artifact reference. The existing `format=pdf` remains
the single-cargo label with `type=simple`; batch printing (`type=multiple`),
formed-cargo cancellation, returns and other writes remain fail-closed.

## Phase 48 — Почта России — чтение партий

`179`

Task 179 is repository-complete. Почта России now exposes the bounded
`logistics.batches.read` route backed by the official `GET /1.0/batch` endpoint.
The API and UI support optional mail type/category filters and bounded page
navigation, while the neutral result contains only batch identity, status,
shipment count and observation time. Batch formation and hand-off remain
fail-closed pending qualification.

## Phase 49 — СБП — admission payment webhook

`180`

Task 180 is repository-complete. SBP's existing `PaymentWebhookVerifier` is
now admitted by the generated built-in runtime support contract and is routed
through the shared public payment receiver. The callback body is not trusted:
the host re-fetches the order status over the account's mTLS channel, then the
receiver records replay evidence and applies only a valid canonical payment
transition. A real acquiring-bank callback contract and non-production account
remain required for live qualification.

## Phase 50 — Telegram HTTPS publication buttons

`181`

Task 181 is repository-complete. Telegram `social.post.buttons` now crosses
the provider-neutral immutable Social Core variant, tenant-scoped PostgreSQL
snapshot, authenticated Social API and leased worker into the existing
HTTPS-only Telegram markup adapter. The `/social` UI validates and submits up
to eight link buttons and renders them in publication history. Callback-data
buttons, edit/delete and inbound webhooks remain fail-closed because they need
separate authorization, callback and reconciliation contracts. Credentialed
live Telegram qualification remains a release gate.

## Phase 51 — MAX HTTPS publication buttons

`182`

Task 182 is repository-complete. MAX `social.post.buttons` is now admitted in
the built-in runtime-support contract and generated catalog. The existing
provider-neutral Social API, account/channel capability gates, leased worker
and `/social` UI expose bounded HTTPS URL buttons through the already tested
MAX inline-keyboard adapter. Webhooks, status reads and destructive mutations
remain outside the connected application runtime; credentialed live MAX
qualification remains a separate release gate.

## Phase 52 — MAX inbound webhook reception

`183`

Task 183 admits the already qualified MAX `social.webhooks` adapter through the
public unauthenticated webhook boundary. The route resolves the tenant and
connector account from the fixed URL, requires the account capability and
passes the ephemeral `X-Max-Bot-Api-Secret` to the connector. The connector
canonicalizes and validates the exact configured channel and update type; the
host claims a minimized `commerce.social.webhook_received.v1` event in the
tenant-scoped Task-009 Inbox and transactional outbox. Raw provider payloads
and verification secrets do not enter durable event data. Subscription
management, edit/delete and live MAX delivery qualification remain separate
release gates.

## Phase 53 — Почта России — формирование партии

`184`

Task 184 admits the existing Russian Post batch formation contract through the
production route. The API requires an enabled `logistics.batches.create`
capability, a matching approved `fulfillment.batch.create` request and an
`Idempotency-Key`. The connector calls `POST /1.0/user/shipment` with a bounded
array of numeric backlog order IDs and returns only a normalized batch
projection. Tenant-scoped operation receipts prevent duplicate external calls;
ambiguous failures remain pending for reconciliation. Handoff to postal work
and separate return shipments stay outside this phase.

## Phase 54 — Почта России — передача партии в работу

`185`

Task 185 admits the Russian Post batch check-in contract through the production
route. The API requires an enabled `logistics.batches.submit` capability, a
matching approved `fulfillment.batch.submit` request and an `Idempotency-Key`.
The connector calls `POST /1.0/batch/{batch-name}/checkin`, optionally passes
`useOnlineBalance=true` and accepts only an explicit `f103-sent` confirmation.
The normalized submission result is stored in the tenant-scoped operation
receipt; completed replays never call the provider again and ambiguous results
remain pending for reconciliation. Separate return shipments remain outside
this phase.

## Phase 55 — Почта России — отдельная возвратная отправка

`186`

Task 186 admits the Russian Post standalone-return contract. The API requires
an enabled `logistics.return.separate.create` capability, matching approval and
an `Idempotency-Key`; the host sends one request item to
`PUT /1.0/returns/return-without-direct` and accepts only `position=0` with a
validated `return-barcode`. Names and addresses are request-scoped and are not
stored in operation receipts; completed replays do not call the provider and
ambiguous outcomes remain pending for reconciliation. Cancellation of an
already formed batch remains outside this phase.

## Phase 56 — Почта России — архивирование сформированной партии

`187`

Task 187 admits the Russian Post formed-batch archive contract. The API
requires an enabled `logistics.batches.archive` capability, matching approval
and an `Idempotency-Key`; the host sends one numeric batch name to the official
`PUT /1.0/archive` endpoint and accepts only an exact `batch-name` response.
The normalized `ARCHIVED` result is stored in the tenant-scoped operation
receipt; completed replays do not call the provider and ambiguous outcomes
remain pending for reconciliation. Restoring an archived batch remains outside
this phase.

## Phase 57 — Почта России — возврат партии из архива

`188`

Task 188 admits the Russian Post formed-batch archive-restore contract. The
API requires an enabled `logistics.batches.unarchive` capability, matching
approval and an `Idempotency-Key`; the host sends one numeric batch name to
the official `POST /1.0/archive/revert` endpoint and accepts only an exact
`batch-name` response. The normalized `RESTORED` result with
`archived=false` is stored in the tenant-scoped operation receipt; completed
replays do not call the provider and ambiguous results remain pending for
reconciliation. Other archive operations remain outside this phase.

## Phase 58 — Почта России — чтение партий из архива

`189`

Task 189 admits the Russian Post archived-batch directory read contract. The
API requires an enabled `logistics.batches.archive.read` capability and a
bounded `limit` up to 100; the host calls the official `GET /1.0/archive`
endpoint over fixed HTTPS egress. The response is reduced to unique batch
reference, status, shipment count and observation time, with no order rows or
raw provider fields crossing the connector boundary. Archive writes and
other archive operations remain separately gated.

## Phase 59 — Почта России — удаление отдельной возвратной отправки

`190`

Task 190 admits the Russian Post standalone-return deletion contract. The API
requires an enabled `logistics.return.separate.delete` capability, matching
approval and an `Idempotency-Key`; the host calls the official
`DELETE /1.0/returns/delete-separate-return?barcode=...` endpoint with no
request body. Only a `2xx` response with an empty body or empty `code` is
accepted and normalized to `DELETED`/`deleted=true`; provider error codes and
invalid barcodes are rejected. The tenant-scoped operation receipt prevents
duplicate calls, while ambiguous results remain pending for reconciliation.
Live qualification must use a disposable test return because provider
deletion is irreversible.

## Phase 60 — Почта России — редактирование отдельной возвратной отправки

`191`

Task 191 admits the Russian Post standalone-return edit contract. The API
requires an enabled `logistics.return.separate.edit` capability, matching
approval and an `Idempotency-Key`; the host calls the official
`POST /1.0/returns/{barcode}` endpoint with the bounded editable payload.
Only a response without errors that confirms the exact same `return-barcode`
is accepted and normalized to `UPDATED`/`updated=true`; empty, malformed or
mismatched responses remain failures. The tenant-scoped operation receipt
prevents duplicate calls, while ambiguous results remain pending for
reconciliation. Live qualification must use a disposable test return.

## Phase 61 — Telegram — редактирование опубликованного сообщения

`192`

Task 192 admits approval-bound editing of one already published Telegram
message through `PATCH /api/v1/social/publications/{publication_id}`. The API
requires the enabled `social.post.edit` capability, the matching approved
write-sensitive request, an immutable remote publication receipt and an
`Idempotency-Key`; the runtime accepts only the Telegram editor and a confirmed
same-message result. A tenant-scoped operation receipt prevents duplicate
provider calls and leaves ambiguous outcomes pending. Telegram deletion and
webhooks remain fail-closed.

## Phase 62 — Telegram — удаление опубликованного сообщения

`193`

Task 193 admits approval-bound deletion of one already published Telegram
message through `DELETE /api/v1/social/publications/{publication_id}`. The API
requires the enabled `social.post.delete` capability, the matching approved
write-sensitive request, an immutable remote publication receipt and an
`Idempotency-Key`; the runtime accepts only the Telegram deleter and a
confirmed same-message deletion result. A tenant-scoped operation receipt
prevents duplicate provider calls and leaves ambiguous outcomes pending.
Telegram webhooks remain fail-closed.

## Phase 63 — Telegram — входящие channel-post webhook

`194`

Task 194 admits verified Telegram `channel_post` and `edited_channel_post`
updates through the existing tenant-bound Social webhook route. The host
extracts the `X-Telegram-Bot-Api-Secret-Token` header, while the connector
compares it against a separate callback-scoped SecretProvider reference,
canonicalizes the bounded JSON and checks the exact configured channel and
message timestamp. A content-addressed claim is deduplicated through the
Task-009 Inbox and transactional outbox. Direct messages, groups, callback
queries, subscription lifecycle and other update types remain fail-closed.

## Phase 64 — Telegram — lifecycle подписки webhook

`195`

Task 195 admits the bounded Telegram webhook subscription lifecycle through
the authenticated host API. `PUT /social/webhooks/subscription` calls the
official `setWebhook` method with only `channel_post` and
`edited_channel_post`; `DELETE` first checks `getWebhookInfo` and calls
`deleteWebhook` only for the exact requested endpoint. Both operations require
an active social account, `social.webhooks`, `connectors.accounts.write`,
durable idempotency and audit. Callback-scoped secrets stay behind
SecretProvider, completed replays do not call Telegram again, and callback
actions plus other update types remain fail-closed.

## Phase 65 — ПЭК — возврат принятого груза отправителю

`196`

Task 196 admits the bounded ПЭК return-to-sender operation through the existing
approval-bound `POST /api/v1/returns/{return_id}/logistics` and durable worker.
The adapter calls the official `POST /api/v1/cargos/cancelandreturncargo/` with
one cargo code and accepts only `success=true`; a confirmed provider rejection
is permanent and a network-ambiguous result remains unknown for reconciliation.
Pre-registration cancellation, formed-cargo cancellation, address delivery,
batch printing and webhooks remain outside this qualification.

## Phase 66 — ПЭК — пакетная печать этикеток заявки

`197`

Task 197 admits the bounded PEK `type=multiple` print operation through the
existing `logistics.label.read` route and explicit `format=multiple_pdf`. The
host sends one numeric cargo code, validates the bounded base64 response as a
PDF and returns only an opaque digest reference. Single-cargo labels and the
request form remain compatible; formed-cargo cancellation and other
unqualified writes remain fail-closed.

## Phase 67 — Почта России — форма Ф103 партии

`198`

Task 198 admits the bounded Russian Post F103 document through the existing
`logistics.label.read` route and explicit `format=batch_f103_pdf`. The host
calls `GET /1.0/forms/{batch-name}/f103pdf` for one numeric batch, validates
the PDF response and returns only an opaque digest reference. Other
unqualified documents remain fail-closed.

## Phase 68 — Почта России — форма сформированного заказа

`199`

Task 199 admits the bounded Russian Post formed-order document through the
existing `logistics.label.read` route and explicit `format=formed_order_pdf`.
The host calls `GET /1.0/forms/{order-id}/forms` for one numeric order after
batch formation, sends `print-type=PAPER` and the current date, validates the
PDF response and returns only an opaque digest reference. Other unqualified
documents remain fail-closed.

## Phase 69 — MAX — редактирование и удаление сообщений

`200`

Task 200 admits bounded approval-bound MAX `social.post.edit` and
`social.post.delete` through the existing Social API and immutable remote
receipt. The host calls `PUT/DELETE /messages?message_id=...` on the fixed
official host, revalidates released media and HTTPS buttons, accepts only
`success=true` and never retries an ambiguous write. Comments, callback
actions and webhook subscription lifecycle remain fail-closed.

## Phase 70 — MAX — жизненный цикл подписки webhook

`201`

Task 201 admits MAX Webhook subscription and unsubscription through the
existing authenticated host route and tenant-scoped operation receipt. The
adapter calls the official `POST/DELETE /subscriptions`, fixes the accepted
update types and verification secret boundary, validates HTTPS endpoints and
does not retry ambiguous writes.

## Phase 71 — 5Post pickup-point directory

`202`

Task 202 admits only the bounded 5Post pickup-point directory read. The host
uses the official v7.32 JWT exchange, requests one bounded page from
`/api/v1/pickuppoints/query`, filters and normalizes the response into the
provider-neutral `PickupPoint` contract, and never returns the JWT or raw
provider payload. Shipment create/cancel, tracking and labels remain
qualification-gated pending separate deterministic fixtures and write-boundary
review.

### Gate RUNTIME-202

- the API key is exchanged through the callback-scoped SecretProvider secret;
- the provider's documented page limit and the host's 500-point result bound
  are enforced;
- only official fixed-host HTTPS requests are used and `accept-language: ru`
  is sent for deterministic error text;
- runtime support, frontend catalog, tests, architecture review and docs are
  synchronized before deployment.

## Phase 72 — 5Post single-order status read

`203`

Task 203 admits one-order 5Post status lookup through the official
`POST /api/v1/getOrderStatus` route. The host exchanges the callback-scoped API
key for a JWT, sends exactly one provider order ID, requires exactly one
matching response and normalizes only the provider status, partner tracking
reference and UTC change date. Shipment create/cancel and label operations
remain qualification-gated.

### Gate RUNTIME-203

- provider order identity is matched before any normalized result is returned;
- provider-local `executionStatus` and rejection text do not enter the Core
  shipment projection;
- malformed dates, duplicate/missing results and mismatched IDs fail closed;
- runtime support, frontend catalog, tests, architecture review and docs stay
  synchronized.

## Phase 86 — Epic 172: Marketplace Product Publication

`217`

Task 217 closes the marketplace product publication contour under the original
Epic 172 title. A versioned provider-neutral snapshot passes Product Quality,
approval and account capability checks before a tenant-scoped idempotent
PostgreSQL worker sends it through the typed connector surface. WB, Ozon and
Yandex Market return normalized asynchronous receipts; unverified providers and
unsupported media/attribute bridges remain fail-closed.

### Gate RUNTIME-217

- Product data crosses the connector boundary only as a validated immutable
  snapshot; provider tokens, raw responses, arbitrary URLs and quarantined
  media do not cross it;
- live writes require an active marketplace account, enabled `products.write`,
  a matching non-stale quality receipt and an approved publication request;
- repeated idempotency keys cannot create a second remote card, while timeout
  outcomes remain `unknown` until read-after-write/reconciliation;
- migration 44 is cataloged with a verified checksum, high-risk rollout backup
  and forced tenant RLS on publication evidence;
- OpenAPI, generated SDKs, connector manifests/support, API/worker/UI,
  deterministic provider fixtures, architecture review and Docker qualification
  remain synchronized.

## Phase 73 — 5Post order cancellation

`204`

Task 204 admits the bounded approval-bound 5Post cancellation request through
`DELETE /api/v2/cancelOrder/byOrderId/{orderId}`. The host exchanges the
callback-scoped API key for a JWT, validates the provider UUID and treats only
the documented `error=false` business response as cancellation success.

### Gate RUNTIME-204

- cancellation remains behind the existing authenticated worker approval,
  tenant, secret and idempotency boundaries;
- provider retryable and terminal business errors remain visible and are not
  projected as cancelled;
- ambiguous transport results are not retried by the connector;
- runtime support, frontend catalog, tests, architecture review and docs stay
  synchronized.

## Phase 74 — 5Post PDF label

`205`

Task 205 admits one 5Post PDF label read through the official
`POST /api/v1/orderLabels/byOrderId?format=PDF` route. The host sends one
provider order UUID, validates the PDF media type and `%PDF-` signature, and
returns only a content-addressed opaque artifact reference. Shipment creation
remains qualification-gated.

### Gate RUNTIME-205

- the label request remains bounded to one provider order and `format=PDF`;
- asynchronous/not-ready and non-PDF responses fail closed;
- the PDF body, API key and JWT never cross the host boundary;
- runtime support, frontend catalog, tests, architecture review and docs stay
  synchronized.

## Phase 75 — 5Post universal one-parcel order create

`206`

Task 206 admits a bounded one-parcel 5Post order through the official
`POST /api/v3/orders` endpoint. The neutral shipment request now carries
explicit declared product lines and money values; account configuration
supplies the provider sender location, undeliverable policy and barcode
enrichment mode. The host accepts only `code=10` with one matching order and
cargo identity and never retries an ambiguous write. Tariff calculation,
returns and webhooks remain fail-closed.

### Gate RUNTIME-206

- product-line totals, currency, VAT and payment-value relationships are
  checked before the external write;
- the provider sender location and barcode contract are explicit tenant
  configuration, not inferred from a neutral address or pickup point;
- only one pickup-point order and one cargo are sent, and response IDs are
  matched before a normalized `created` result is returned;
- runtime support, OpenAPI, frontend catalog, tests, architecture review and
  docs stay synchronized.

## Phase 76 — 5Post C2C tariff preview

`207`

Task 207 admits the bounded 5Post C2C tariff endpoint through the official
`POST /api/v1/tariff/c2c`. The neutral rate request carries explicit placement
and issue point UUIDs plus optional declared/payment values; the host converts
SDK grams to provider milligrams, parses decimal prices exactly into RUB minor
units and bounds the returned delivery days. Other commercial tariffs remain
fail-closed.

### Gate RUNTIME-207

- point UUIDs are explicit and are never inferred from city/address text;
- provider decimal values are not decoded through binary floating point;
- malformed, missing or out-of-range response costs and delivery periods fail
  closed;
- runtime support, OpenAPI, frontend catalog, tests, architecture review and
  docs stay synchronized.

## Phase 77 — Деловые Линии terminal-to-terminal shipment create

`208`

Task 208 admits the bounded terminal-to-terminal variant of the existing
Деловые Линии shipment-create route. `sender_terminal_id` is explicit tenant
runtime configuration and `pickup_point_ref` is the numeric receiver-terminal
reference. Address-to-address creation remains unchanged; terminal payloads
contain `variant=terminal` and both terminal IDs without address objects.

### Gate RUNTIME-208

- both terminal references are explicit, numeric and provider-owned; they are
  never inferred from Core addresses or warehouse IDs;
- the common approval-bound worker, SecretProvider, tenant isolation and
  no-retry-on-ambiguous-result boundaries remain in force;
- deterministic fixtures prove terminal payload shape and preserve the
  address-to-address payload;
- runtime support, frontend configuration, generated catalogs, tests,
  architecture review and connector qualification docs stay synchronized.

## Phase 78 — Деловые Линии pickup cancellation

`209`

Task 209 admits the bounded `delivery`/`pickup` variants of the existing
approval-bound Dellin cancellation route. The default remains `delivery`; the
new `pickup` mode calls the official `/v3/orders/cancel_pickup.json` endpoint
and carries the selected mode through the tenant-scoped event to the worker.
Both provider responses remain `cancellation_pending` until reconciliation.

### Gate RUNTIME-209

- the variant is a bounded enum and invalid values fail before connector
  invocation;
- Dellin uses only the documented delivery and pickup cancellation endpoints,
  with numeric order identity and `requester=sender`;
- approval, tenant, SecretProvider, idempotency and no-blind-retry boundaries
  remain unchanged;
- OpenAPI/event schemas, generated SDKs, frontend catalog, deterministic
  transport/worker tests, architecture review and qualification docs stay
  synchronized.

## Phase 79 — Почта России: возврат заказов из партии в «Новые»

`210`

Task 210 admits the official Russian Post `POST /1.0/user/backlog` operation
through a new neutral `logistics.orders.restore` capability. The route is
approval-bound and idempotent, sends 1–100 numeric order IDs, and accepts only
an exact full `result-ids` acknowledgement without provider errors. This is
distinct from cancellation and parcel return.

### Gate RUNTIME-210

- partial, duplicate, mismatched or provider-error acknowledgements fail closed;
- the existing tenant, approval, SecretProvider and no-blind-retry boundaries
  remain unchanged;
- OpenAPI, generated SDKs, runtime support, frontend, deterministic transport/
  connector/API tests, architecture review and qualification docs stay synchronized;
- the operation remains separately removable without changing batch creation,
  cancellation or return semantics.

## Phase 80 — Почта России: чтение заказов внутри партии

`211`

Task 211 admits the bounded read-only Russian Post batch-order endpoint through
`logistics.batches.orders.read`. The host calls
`GET /1.0/batch/{batch-name}/shipment`, validates the numeric batch identity,
page bounds and exact response membership, then exposes only provider order ID,
batch ID, barcode, normalized lowercase status and UTC observation time.

### Gate RUNTIME-211

- recipient/address fields and raw provider payload never cross the connector
  boundary;
- mismatched batch IDs, duplicate rows, malformed identifiers/statuses and
  responses larger than the requested limit fail closed;
- the route remains read-only and does not require approval or idempotency
  receipts;
- OpenAPI, generated SDKs, runtime support, frontend catalog/UI, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 83 — Почта России: поиск заказа по номеру магазина

`214`

Task 214 admits the bounded read-only Russian Post backlog search through
`logistics.orders.search`. The host calls `GET /1.0/backlog/search` with the
merchant order number, enforces exact external-ID membership and a maximum of
100 results, and exposes only stable references, status and observation time.

### Gate RUNTIME-214

- the route is read-only and performs no approval, idempotency or provider write;
- the query is bounded and provider results must match the requested merchant
  order number exactly;
- duplicate rows, malformed references/statuses and oversized responses fail
  closed;
- recipient/address fields and raw provider payload never cross the connector
  boundary;
- OpenAPI, generated SDKs, runtime support, frontend catalog/UI, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 81 — Почта России: поиск одного заказа в партии

`212`

Task 212 admits the bounded Russian Post `GET /1.0/shipment/{id}` lookup through
`logistics.orders.read`. The host accepts only a numeric provider order ID,
supports one object or one-item array response, requires exact ID membership and
projects no recipient/address fields.

### Gate RUNTIME-212

- the route is read-only and performs no approval, idempotency or provider write;
- response ID, batch ID, barcode and status are validated and status is exposed
  in normalized lowercase form;
- missing/mismatched IDs, multiple rows and malformed responses fail closed;
- OpenAPI, generated SDKs, runtime support, frontend UI/catalog, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 84 — Почта России: изменение даты передачи партии

`215`

Task 215 admits the approval-bound Russian Post batch hand-off date update
through `logistics.batches.sending_date.write`. The host calls the official
path-only `POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD`, accepts the
documented empty success acknowledgement, and exposes only the exact batch ID,
ISO date and normalized `UPDATED` result.

### Gate RUNTIME-215

- the route requires an enabled write-sensitive capability, matching approval,
  workspace scope and tenant-scoped idempotency receipt;
- the provider request contains no caller-controlled host, query or body, and
  only the documented Russian Post endpoint is admitted;
- empty successful responses and JSON without `error-code` are accepted, while
  provider errors, malformed responses, invalid dates and mismatched IDs fail
  closed;
- raw provider payload, credentials and unrelated batch data never cross the
  connector boundary;
- OpenAPI, generated SDKs, runtime support, frontend catalog/UI, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 82 — Почта России: поиск партии по имени

`213`

Task 213 extends the admitted `logistics.batches.read` capability with the
bounded Russian Post `GET /1.0/batch/{batch-name}` lookup. The host accepts only
a numeric batch name and a single exact response projection containing the
provider batch ID, status, shipment count and UTC observation time.

### Gate RUNTIME-213

- the route is read-only and performs no approval, idempotency or provider write;
- object or single-item array responses are bounded to one exact batch;
- mismatched names, multiple rows and malformed status/count fail closed;
- order rows, raw provider payload and credentials never cross the host boundary;
- OpenAPI, generated SDKs, runtime support, frontend UI/catalog, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 85 — «Деловые Линии»: отмена Pre-Alert пакетной заявки

`216`

Task 216 admits the official Dellin Pre-Alert batch dissolution operation via
`logistics.batches.cancel`. The approval-bound route calls
`POST /v2/batch_request/cancel.json`, validates the numeric `batchRequestID` and
accepts only `metadata.status=200` with `data.state=success`. The normalized
result is `CANCELLED`; this operation is deliberately distinct from cancelling
an individual terminal shipment or creating a manual return.

### Gate RUNTIME-216

- the route requires workspace scope, an active Dellin account, enabled
  write-sensitive capability, matching approval and tenant-scoped idempotency;
- only the fixed official Dellin HTTPS endpoint is admitted and credentials are
  callback-scoped through SecretProvider;
- non-numeric IDs, provider errors, malformed responses and ambiguous transport
  outcomes fail closed, while raw provider payload and batch contents remain
  outside Core;
- OpenAPI, generated SDKs, runtime support, frontend/catalog, deterministic
  transport/connector/API tests, architecture review and qualification docs stay
  synchronized.

## Phase 86 — Supplier & Procurement Operations

`218`

Task 218 closes the user-facing Epic 173 because repository task key 173 is
already occupied by the PЭК shipment task. The implementation adds a
tenant-scoped procurement workbench around the existing PurchaseOrder lifecycle:
canonical LegalParty-linked suppliers, versioned offers, released CSV/XLSX
price-list preview/commit, recommendation snapshot checks, approval-bound
send/unknown/retry, WMS receiving evidence and reconciliation.

### Gate RUNTIME-218

- every supplier, offer, preview, PO and finding query is tenant-scoped with
  forced RLS; mutation metadata is audited and published through transactional
  outbox;
- raw uploads are read only from released objects and verified by digest; raw
  file content, credentials and unnecessary PII never enter ordinary API,
  events or evidence;
- matching uses GTIN, supplier SKU and explicit manual mapping, while invalid
  and ambiguous rows stay outside offer mutation;
- existing PO lifecycle is preserved, approval is bound to the same PO, retry
  is idempotent and timeout is represented as `unknown`;
- receiving produces a WMS-consumable fact and never mutates stock directly;
- OpenAPI, generated SDKs, frontend workbench, migration catalog, tests and
  architecture review stay synchronized. External EDO/marking/marketplace
  writes remain denied until separate qualification.

## Phase 87 — Автоматическая финансовая аналитика продавца

`219`

Task 174 extends Task 167 with a normalized, deterministic seller-finance
layer. It materializes management P&L, cash flow, unit economics and quality
from canonical PostgreSQL facts, values historical COGS through FIFO evidence,
stores immutable calculation snapshots, exposes bounded reports and keeps the
daily worker independent from transactional writes.

### Gate RUNTIME-219

- P&L, settlement and cash use explicit bases; payout is not revenue and
  settlement components cannot be double-counted with payout;
- all money stays in integer minor units with currency, quantities stay exact
  fixed-point, and missing COGS/FX/attribution is visible as quality rather
  than silently converted to zero;
- FIFO evidence is historical/as-of, supports partial consumption and
  cross-warehouse transfer layers, and an unavailable layer fails closed;
- calculation runs are tenant-scoped, idempotent and immutable, with formula,
  allocation, valuation and attribution versions, input digest, coverage and
  quality evidence;
- reports, details and exports are bounded by period/limit and permissions;
  snapshots contain no raw provider payloads, tokens or bank details;
- migration 46 is expand-only and requires backup; rollback is capability
  disablement and worker drain, without destructive down migration;
- OpenAPI, generated SDKs, frontend, migration catalog, worker, tests and
  architecture review stay synchronized. Live bank and external provider
  adapters remain explicitly deferred for separate qualification.

## Phase 88 — Marketplace Advertising Runtime для WB/Ozon

`220`

Task 220 closes the read-only MVP of Epic 175. WB and Ozon expose `ads.read`
through the built-in Connector SDK; the host stores normalized campaigns,
spend/performance facts, sync watermarks and reconciliation evidence in
tenant-scoped PostgreSQL. The worker loads the previous UTC day, and P&L uses
the API facts without adding the same advertising spend on top of older
action/settlement copies. The frontend «Реклама» page shows campaign metrics,
ROAS/ROMI/ДРР, quality and findings.

### Gate RUNTIME-220

- WB and Ozon official read adapters use fixed hosts, bounded periods, scoped
  secrets, normalized errors and `ads.read`; provider fields do not cross the
  adapter boundary;
- campaign and fact identities are tenant-scoped, spend/performance facts are
  immutable and repeated daily ingestion is idempotent;
- advertising API facts participate in the existing P&L with explicit source
  precedence, while unattributed SKU and delayed data remain visible findings;
- API, sync-run evidence, generated Go/Python/TypeScript SDKs, frontend route,
  permissions, migration catalog and forced-RLS checks stay synchronized;
- migration 47 requires a PostgreSQL backup and rollback is capability
  disablement plus worker drain; no destructive down migration is used;
- campaign launch/stop, budget, bid and product-link writes are not admitted
  in this gate and require a later approval-bound qualification.

## Phase 89 — Epic 176: Marketplace Operations v1

`223`

Task 223 is the parent integration gate for the complete marketplace product,
not a second domain implementation. It composes the canonical product,
pricing/inventory, order, WMS/fulfillment, return, marking/EDO, settlement,
advertising and financial boundaries. The user-facing Epic number is `176`;
repository task key `223` is required because keys `176–222` already exist.

### Gate RUNTIME-223

- the end-to-end scenario is explicit:
  account → product → publication → price/stock → order → reserve → pick/pack
  → shipment → return → settlement → P&L;
- every provider reports `read_only`, `partially_supported` or `qualified`
  from actual capabilities and evidence; manifest, SDK types, health checks and
  credentials do not imply full support;
- cross-domain state uses existing PostgreSQL aggregates, EventBus/outbox,
  Inbox/deduplication, tenant scope and reconciliation rather than a duplicate
  marketplace ledger;
- every write-sensitive operation is capability/policy/approval/idempotency
  gated, timeout after remote acceptance becomes `unknown`, and stale or
  partial results remain visible findings;
- the gate covers duplicate/replay, crash before/after remote acceptance,
  timeout/rate-limit, out-of-order status, partial return/refund, stale
  price/stock, missing mapping, cross-tenant IDs and secret-leak checks;
- account settings, operations center, UI, API/SDK changes and provider
  qualification are admitted only in child slices with their own contracts,
  migrations, tests and rollback evidence.
