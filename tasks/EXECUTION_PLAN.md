# Execution Plan

This is the canonical sequential linearization of the TORGNEXA backlog. A task starts only after all preceding mandatory dependencies are complete and closes only after its acceptance checks pass. Parallel implementation is allowed only after the relevant gate and must not concurrently change shared contracts.

Tasks `076`, `088`, and `089` are explicitly split into `a` and `b` implementation stages below. The numbered parent task closes only at stage `b`.

## Progress

- Completed repository implementation: `001`, `024`, `065`, `002`, `027`,
  `067`, `080`, `003`, `021`, `060`, `007`, `008`, `009`, `004`, `005`, `006`, `076`, `025`, `010`, `029`, `064`, `017`, `030`, `023`, `081`, `082`, `028`, `026`, `063`, `022`, `062`, `032`, `031`, `088`, `013`, `014`, `011`, `012`, `015`, `016`, `033`, `034`, `035`, `036`, `018`, `079`, `020`, `019`, `078`, `040`, `041`, `042`, `037`, `038`, `039`, `043`, `044`, `045`, `046`, `047`, `048`, `049`, `050`, `051`, `052`, `053`, `054`, `055`, `056`, `057`, `058`, `059`, `061`, `066`, `068`, `069`, `070`, `071`, `072`, `073`, `074`, `075`, `077`, `083`, `084`, `085`, `086`, `087`, `090`, `091`, `092`, `089`, `093`, `094`, `095`, `096`, `097`.
- Completed split-stage repository implementation: `076a`, `076b`, `088a`, `088b`, `089a`, and `089b`; parent Tasks `076`, `088`, and `089` are repository-complete.
- Contiguous implemented baseline: Tasks `001`–`133`. Task `118` closes the P4 repository layer with fail-closed go-live evidence synthesis and PASS-gated release promotion; Tasks `119`–`130` add operator UX, compact migrations, AI/MCP governance, the trust control plane and a runtime-truthful integration catalog; Tasks `131`–`133` compose CBR FX, Telegram and MAX into truthful dedicated production surfaces. Deployment/hosted and live-provider evidence remains release-topology specific and cannot be inferred from repository completion.
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
generic product integrations as executable, direct six AI connectors to their
dedicated surface and keep 21 manifest-only connectors visibly planned but
non-connectable. AliExpress RU, Magnit Market, Megamarket, OpenCart and
PrestaShop join the built-in product runtime without broadening the worker past
the canonical `products` entity.

### Gate RUNTIME-130

- manifest, runtime contract, generated TypeScript and generated Go inventories
  must contain the same 38 connector IDs;
- operational capabilities must be manifest subsets and unsupported
  connector/entity/direction requests must fail closed at API and worker
  boundaries;
- all 11 ready connectors resolve product readers, while only OpenCart and
  WooCommerce admit outbound product synchronization;
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
132. Only `social.post.text` is executable: the provider ceiling is 4000 Unicode
code points and the host permits only exact account/channel health reads plus
`POST /messages?chat_id=...`. Runtime inventory becomes 11 generic product
integrations, nine working separate-surface providers and 18 planned entries.

### Gate RUNTIME-133

- bot tokens remain callback-scoped and `chat_id` is strict non-zero non-secret
  configuration;
- API/Core/worker remain provider-neutral and MAX protocol branching stays in
  built-in composition;
- uploads, media, status and webhooks fail closed despite their SDK presence;
- Task-132 ambiguous-write recovery cannot duplicate a remote message;
- generation, Go test/vet, contracts, architecture, frontend build and live MAX
  qualification must pass before a deployment claims complete production proof.
