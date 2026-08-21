# TORGNEXA handoff — MCP identity + AI provider connectors + P4 + Enterprise UX + pre-v1 migration baseline

Date: 2026-08-20

## Current state

Tasks `001`–`128` are repository-implemented. Task 118 adds the fail-closed P4 go-live evidence/publication layer; Task 119 closes the base operator UI/UX gap; Task 120 upgrades enterprise operations with server-owned grids, realtime invalidation, unified incidents, deep/server search and reporting-backed analytics; Task 121 replaces the 74-file development migration install path with an 11-file pre-v1 baseline and verified legacy rebaseline; Task 122 admits the tenant-scoped AI provider settings/analyze capability and its first provider, `openai-compatible`; Tasks 123–125 add Kimi, GigaChat and YandexGPT as three further `ai`-family providers on the same capability; Task 126 gives `cmd/mcp` its first non-deny `IdentityResolver` via a new tenant-scoped MCP client account capability; Tasks 127–128 add Qwen and DeepSeek as two further `ai`-family providers on the Task-122 capability.

Current repository inventory:

- architecture: **121 registered modules / 38 provider modules / 119 reviews**;
- active PostgreSQL baseline: **15 migrations**, latest `000015_ai_provider_qwen_deepseek.sql`; archived immutable pre-v1 lineage: **74 migrations**, legacy head `000074_fulfillment_failover_execution.sql`;
- public OpenAPI/generated SDK surface: **121 operations / OpenAPI 0.20.0**;
- connector catalog: **38 connectors**;
- repository license: **Apache-2.0**.

Repository completion is still distinct from release-topology qualification. Task 117 makes runtime qualification mandatory in the release workflow, but this source archive cannot manufacture Docker, OIDC, GitHub Ruleset or live provider evidence.

## Post-126 session: sdkgen parser gap, AgentGovernor composition, upload→product-image wiring, MCP agent policy admin surface

Four fixes landed in the same working session, in dependency order.

`cmd/mcp`'s `Run()` composed a real `agentgovernance.Service`/`audit.Service` in place of the `unavailableGovernor{}`/`unavailableAuditor{}` stubs Task 126 shipped with — MCP tool calls are now genuinely policy-checked rather than always denied.

ADR 0098's own "Consequences" section flagged what this immediately surfaced live: even a valid, enabled `mcp_client_accounts` token got an empty `tools/list` and a denied `tools/call`, because nothing anywhere installs the `agentgovernance.Policy` the new governor requires, and the ADR explicitly deferred building that admin surface. It is no longer deferred: `internal/app/api/mcp_agent_policies.go` adds `GET/POST /settings/mcp-accounts/{account_id}:policy|:install-policy` and `GET/POST /settings/mcp-agents:kill-switch` (permissions `settings.mcp_accounts.read`/`.write`, matching the account capability exactly). Installing a policy never accepts risk or approval-required as input — those are fixed per MCP tool in `internal/app/mcp/tools.go` and the handler derives them from that same catalog, so an install can never produce a rule the governance evaluator would silently mismatch; the only real input is the spending limit for the one sensitive-write tool (`commerce.price.change.request`), which `ToolRule.Validate()` requires before an install can succeed. The kill switch is deliberately tenant-wide only (an agent-level or integration-level one would be redundant with the account's own `enabled` flag, which already stops authentication outright) and reuses `agentgovernance.AgentKillState` with a probe agent id that can never collide with a real one to read the tenant row's version without a second repository method. `MCPAccountSettings.tsx` gained a per-account "Политика доступа" panel and a tenant-wide emergency-stop control at the top of the MCP-агенты tab.

`tools/sdkgen`'s hand-rolled OpenAPI parser (regex-based, not a real YAML parser) silently produced zero parameters for two spec shapes it had no pattern for: the single-line inline-array `$ref` form (`parameters: [{$ref: '#/components/parameters/IdempotencyKey'}]`, used by 26 write operations) and any parameter written without an explicit `required:` key (valid OpenAPI — omitted means `false` — used by `listFXRates`/`listSettlementEntries`/`listPlugins`/`listCounterparties`). All three generated SDKs (Go/TypeScript/Python) silently dropped the affected Idempotency-Key headers and query filters; the existing test fixture only ever exercised the multi-line `- {$ref: ...}` shape, so the gap was invisible to `make sdk-check`. Fixed in `tools/sdkgen/main.go` with two regression tests locking in the previously-broken shapes; all three SDKs regenerated.

`createUpload`'s quarantine→ClamAV-scan→immutable-evidence→release pipeline (ADR 0030, `internal/platform/uploads`) was fully built and running in the worker but had no consumer — `docs/62-upload-security-pipeline.md` and the ADR both flagged consumer wiring as explicitly deferred. Product images are now that first consumer: `ImageWrite` accepts `upload_id` as an alternative to `url`; the API resolves it via `AccessGate.ResolveReleased` (re-validated on every read, not cached) and stores the exact server-relative content path (`uploads.ContentPath`) rather than trusting any client-supplied location. Two new operations, `getUpload` (lifecycle status, for the frontend to poll) and `getUploadContent` (streams the released bytes with a server-sniffed `Content-Type`), are gated by `products.read` — not `uploads.*` — because the only consumer today is "can you see this product's photos," which every role that can see the product already satisfies. Garage has no public-read/website mode configured, so the frontend fetches upload-backed images with an authenticated request and renders them via a blob URL rather than a plain `<img src>` hotlink; externally hosted `https://` image URLs are untouched and still render directly. No migration was needed — `catalog_product_images.url` already accepted arbitrary text; `catalogimagerepo.validImage` now accepts the one exact internal content-path shape as an alternative to a real `https://` URL, nothing looser.

Verified: `go build/vet/test ./...`, `tools/contractcheck`, `tools/architecturecheck` (0 new reviews required for the first three changes — those stayed inside already-reviewed module boundaries), `tools/sdkgen --check` (no drift), and the full frontend suite (`tsc` × 2 configs, 23/23 `node --test`, production Vite build, static frontend policy) — all green in this workspace, re-run after each change above. Not verified: a live end-to-end upload through the running Community stack (ClamAV/Garage reachability) or a live `tools/list`/`tools/call` round-trip after installing a policy through the new endpoints, since this workspace has no running deployment this session.

## Tasks 127–128 Qwen and DeepSeek connectors

Both admitted the same way Tasks 123–125 admitted Kimi/GigaChat/YandexGPT: Connector SDK v1, `ai.completion.generate` only, no `net/*` import, OpenAI-compatible wire format re-declared locally per provider (connector packages may not import each other) rather than shared. `qwen` defaults to DashScope's OpenAI-compatible mode host `dashscope.aliyuncs.com` with the compatible-mode path `/compatible-mode/v1/chat/completions` (distinct from the plain `/v1/chat/completions` every other provider here uses) and an account-configurable hostname override (for example to the international DashScope region); `deepseek` defaults to `api.deepseek.com`. `internal/platform/builtinruntime.Registry.AICompletion` gains two more `case` arms; `ai_provider_accounts.provider`'s CHECK constraint is widened by migration `000015_ai_provider_qwen_deepseek.sql` (verified applied against a live PostgreSQL 18 instance alongside the full active migration chain, 15/15, and confirmed live via `pg_get_constraintdef`) rather than edited into migration `000012` in place, since applied migration history is immutable. Each provider has its own `ARCH-12{7,8}` architecture review (reusing ADR 0097 rather than reopening it) and Task-064 conformance report, both 13/13 PASS from a real `conformance.Run()` execution — including `sandbox_isolation`, which needs a privileged Linux runner — against a synthetic fixture transport that performs no real network I/O.



## Task 126 MCP client accounts and identity resolver

Since Task 018 shipped, `cmd/mcp`'s `Run()` hardcoded `IdentityResolver: denyIdentityResolver{}` — every `POST /mcp` request was rejected. The blame fell on Task 084 (Enterprise IAM federation), but Task 084 (`internal/platform/enterpriseiam`) turned out to only implement in-memory SSO claim-to-role mapping for human identities, with no Postgres persistence anywhere and no concept of a machine credential.

`mcp_client_accounts` (migration `000014_mcp_client_accounts.sql`, RLS forced) is a new tenant-scoped capability built for this instead: `internal/platform/mcpaccounts` (validation + bearer-token encode/hash) and `internal/platform/postgres/mcpaccountsrepo`, exposed through three additive OpenAPI 0.17.0 operations under `/settings/mcp-accounts(:disable)`. Unlike every other credential in this repository, the token is inbound (an agent presents it *to* TORGNEXA), so only a SHA-256 `token_hash` is stored and the raw token is shown exactly once, at creation. Because MCP carries no JWT the way REST does, the token itself embeds organization/workspace/account IDs so `internal/app/mcp/identity.go`'s `PostgresIdentityResolver` can build a `tenancy.Scope` before any RLS-scoped query — the embedded IDs are only a routing hint; a constant-time hash comparison is what actually authenticates. `PostgresIdentityResolver` now replaces `denyIdentityResolver{}` in `cmd/mcp`'s `Run()`.

Verified live against the running Community Docker stack with a real Keycloak-authenticated PKCE session (not a stub): created/listed/disabled an MCP account via REST, and `POST /mcp tools/list` with the issued token returned **HTTP 200 with a resolved identity** — the first request this repository's MCP endpoint has ever accepted. A tampered token and a disabled account's token both correctly returned 401.

Discovered while implementing this, and explicitly left open (ADR 0098): Task 079's real `AgentGovernor` (`internal/platform/agentgovernance`, backed by the already-implemented and tested `internal/platform/postgres/agentgovernancerepo`) was never composed into `cmd/mcp` either. With only Task 126 applied, `tools/list` returns an empty tool array and `tools/call` is denied even for a valid, enabled account — confirmed live, not just inferred. Frontend: new "MCP-агенты" settings tab (`MCPAccountSettings.tsx`) with per-account tool-permission checkboxes and a one-time token-reveal dialog.

## Tasks 122–125 AI provider settings and connectors

`ai_provider_accounts` (migration `000012_ai_advisory.sql`, RLS forced, `DELETE`/`TRUNCATE` revoked) lets a tenant configure an external AI provider account — label/model/base_url/folder_id/enabled/version plus a `secrets.Reference` — and trigger a bounded analytics completion through it. Credential bytes never reach Postgres; migration `000013_ai_provider_credential_class.sql` additively widens `secret_references.class` with `ai_provider_credential` for this purpose. Four additive OpenAPI 0.16.0 operations sit under `/settings/ai-providers(:disable|:analyze)`: account management is gated by `settings.ai_providers.read`/`write` (admin), triggering a completion is gated separately by `ai.analyze` (admin/manager/operator). `internal/platform/aiadvisory` is a non-branching port; `internal/platform/builtinruntime.Registry.AICompletion` remains the sole `switch account.ConnectorID` dispatch point for the capability, exactly as ADR 0090 requires for every other built-in provider.

Four `ai`-family providers are admitted on this capability, each through Connector SDK v1 with only `ai.completion.generate` and no `net/*` import — all socket I/O is host-mediated:

- `openai-compatible` (Task 122) — the first admitted provider; establishes the capability itself (ADR 0097);
- `kimi` (Task 123) — Moonshot AI, OpenAI-compatible wire format re-declared locally (connector packages may not import each other), default host `api.moonshot.ai`;
- `gigachat` (Task 124) — Sber; `Complete()` performs a per-call OAuth exchange against `ngw.devices.sberbank.ru` followed by a Bearer completion call against `gigachat.devices.sberbank.ru`; the exchanged token is used once and never persisted;
- `yandexgpt` (Task 125) — folder-scoped `gpt://<folder_id>/<model>` URIs against `llm.api.cloud.yandex.net`; `Health()` keeps the frozen 3-argument `sdk.Connector` shape, with the live folder-scoped probe exposed separately as `HealthCheckWithFolder` inside `builtinruntime` only.

Create/disable/analyze audit entries are write_sensitive and record only actor, correlation id and a bounded outcome summary (provider, label, ok) — prompt/response text is never audited, so the capability cannot become an unbounded data-egress channel through audit storage. Each of the four providers has its own `ARCH-12{2,3,4,5}` architecture review, ADR 0097 (123–125 reuse it rather than reopening it), capability audit/spec docs and a Task-064 conformance report, each independently 13/13 PASS.

## Task 121 Pre-v1 migration baseline

The fresh-install migration path is compacted from 74 development migrations to **11 active baseline files**. Bootstrap `000001`–`000003` is retained byte-for-byte; active `000004`–`000011` is generated deterministically from legacy source ranges. The original 74 SQL files/catalog are immutable under `migrations_legacy_pre_v1/`, outside the runtime migration mount.

`make migration-baseline` verifies every archived SHA-256, the 11-file inventory, the source-range manifest and deterministic generated output. An existing development database already at legacy head 74 must use the explicit verified `make migration-rebaseline` path: all 74 history rows are checked, archived in `migration_history_legacy_pre_v1`, and only then are the 11 active baseline stamps written transactionally with `migration_baseline_evidence`. Partial/drifted history remains blocked. This exception is pre-v1-only; after the first production release migration history is immutable.

Local Task 121 gates verify the compact catalog, archived checksums and baseline generation; ARCH-121 governs the migration-lineage change and the exact Task 120 → Task 121 diff passes with 159 changes. Deployment-level rebaseline against a real legacy PostgreSQL database remains an operator action and is never fabricated by repository tests.

## Task 120 Enterprise Operations UX

Catalog and Orders now use server-owned PostgreSQL text/status/cursor pages rather than filtering a bounded browser sample. The protected `/api/v1/realtime` SSE endpoint sends only tenant-scoped invalidation/liveness metadata; React Query then rereads normal authorized APIs. Audit-backed invalidations are low-latency and heartbeat frames provide a bounded refresh fallback for worker-originated state.

The new Incident Center composes warehouse incidents, open drift, degraded connector accounts and pending approvals. Catalog/Order/Incident URLs are durable deep links. Command Palette product/order search is server-side and capability-aware. Dashboard order/GMV KPIs come from the replay-safe reporting projection and Reports use accessible SVG analytics with 7/30/90-day presets. Task 120 adds one additive OpenAPI operation (`streamRealtimeInvalidations`), moves the contract to 0.15.0 / 108 generated operations, and adds no database migration or event schema.

## Task 119 UI product experience

The frontend now presents TORGNEXA as an operator console rather than an engineering shell. The dashboard surfaces order activity, degraded connectors, reconciliation drift, warehouse incidents and pending approvals; first-run onboarding is derived from real connector/warehouse/sync state. Shared primitives now include dependency-free SVG icons, semantic design tokens, light/dark presentation, density controls, skeletons, toasts, Drawer/Dialog, a reusable DataTable, an Activity Center and a capability-aware `Cmd/Ctrl+K` global search.

Catalog, Orders and Inventory preserve list context while details open in drawers. Inventory exposes durable warehouse incidents and fulfillment allocation replacement lineage; Sync exposes drift comparison; Integrations is overview-first with account configuration isolated in a connector drawer. DataTable views are bookmarkable URL state only: Task 119 does not add local/session storage, cookies, tenant selectors, API operations or migrations. Keyboard `G <key>` navigation, labelled mobile navigation, focus-visible and reduced-motion handling are regression-tested.

## P4 go-live evidence

`make p4-qualification` is the final release/topology/hosting gate. It requires the exact clean release tag, Go 1.26.5, Docker, a real production posture statement, GitHub applied branch rules, protected release evidence, live connector accounts and environment-only credentials. It re-runs P3, independently verifies Sigstore/SLSA identity, compares GitHub draft asset SHA-256 digests with the locally verified release bytes, and performs two consecutive remote health checks for every active production connector account; omission of any active account is a qualification failure.

The protected release workflow now stages a **non-public draft** after its internal verify job. It does not publish that draft. Only a retained `p4-go-live.json` with `status: PASS` can be supplied to `make p4-publish`, which then re-verifies all subordinate evidence hashes, proves the draft asset set/digests are unchanged, uploads `p4-go-live.json`, and clears the draft flag.

`.github/workflows/architecture-required.yml` is the canonical ruleset-workflow source for ARCH-OPS-01. P4 requires the hosting platform to report it as an active SHA-pinned `workflows` rule, together with deletion/non-fast-forward protection, PR approvals and a required Team reviewer for architecture paths.

## P3 warehouse execution

`fulfillment_allocations` is now the authoritative binding between an immutable order item and its reserved warehouse. A normal reservation:

1. locks the order item and target inventory position;
2. verifies the order is non-terminal and warehouse is active/degraded;
3. requires sufficient ATP;
4. increases `reserved` on the exact position;
5. creates one active allocation for the order item;
6. emits inventory and fulfillment allocation Outbox evidence.

For a `UNAVAILABLE`/`LOST` warehouse incident the worker processes tracked allocations transactionally:

- lock source allocations and inventory;
- sum the exact tracked reservation quantity;
- select only an explicitly configured active/degraded backup with enough ATP;
- lock/recheck destination ATP;
- decrement source reserved and increment destination reserved by the same exact quantity;
- mark source allocations `released`;
- create immutable replacement allocations at the destination with `incident_id` + `replaces_allocation_id` lineage;
- emit inventory-position and `commerce.fulfillment.allocation_changed.v1` events;
- commit all changes together.

Physical `on_hand` is never transferred or fabricated. Terminal orders, inconsistent reservation accounting, insufficient capacity and legacy untracked reservations fail closed into execution attention instead of guessing.

## Release qualification

The protected `.github/workflows/release.yml` now contains a mandatory `runtime-qualification` job. `build` depends on it, so release bytes cannot be produced by that workflow until the deployed-image qualifier passes.

`make production-qualification` proves:

- Outbox -> Kafka -> Inbox;
- duplicate event idempotency and continued marker progress;
- tracked warehouse reservation reroute;
- source physical stock unchanged;
- source reservation released;
- destination reservation created;
- fulfillment allocation Outbox evidence observed;
- recovery after worker, Kafka and PostgreSQL restarts;
- bounded API availability/latency/throughput.

`make p3-qualification` additionally runs the full repository checks plus PostgreSQL backup/restore and upgrade rehearsal, and writes combined evidence under `qualification/evidence/`.

## License and publication

`LICENSE-DECISION.md` is resolved and the top-level repository `LICENSE` is Apache-2.0. `public_release_ready:true` now means only that the repository-license metadata blocker has been removed.

Repository P4 machinery is complete, but public promotion remains independently blocked whenever any of the following real-environment facts is missing/failing:

- Task 065 protected OIDC signing/provenance and current vulnerability/image evidence (verified from the exact protected release evidence);
- Task 080 hosted Required Workflow / branch Ruleset / reviewer evidence (captured from GitHub applied rules);
- exact release-topology Docker P3 qualification;
- full Go 1.26.5 test/vet/check suite;
- release backup/restore and upgrade evidence;
- live connector qualification where seller/provider credentials are required.

## Validation performed in this workspace

Available local checks for the P4 repository delta:

- P4 fail-closed policy tests: **PASS — 5 tests**;
- deterministic release-evidence packaging: **PASS**;
- P3→P4 architecture diff: **PASS — 33 changed files / exact ARCH-118 scope**;
- migration catalog/baseline: **PASS — 14 active migrations / latest 000014; legacy 74-file head pinned and archived**;
- architecture policy: **PASS — 121 modules / 36 providers / 117 reviews**;
- generated public SDKs: **PASS — 115 operations / OpenAPI 0.17.0**;
- frontend shell/catalog/static policy: **PASS — 23/23 / 36 connectors**;
- Task-064 provider conformance for the four new `ai`-family connectors (`openai-compatible`, `kimi`, `gigachat`, `yandexgpt`): **PASS — 13/13 each**;
- Task 126 MCP client account identity path: **PASS — verified live** (real Keycloak-authenticated session, real issued MCP bearer token accepted end-to-end, tampered/disabled tokens correctly rejected), not only unit tests;
- JS supply-chain repository/lock and Community deployment policies: **PASS**;
- release and required-workflow YAML/P4 static invariants: **PASS**;
- all new P4 shell/Python source syntax checks: **PASS**.

The host still exposes Go 1.23.2 and has no Docker command/network access to fetch the pinned Go 1.26.5 toolchain or missing modules. Therefore this handoff does not claim a fresh full-tree Go 1.26.5 PASS or deployment-level P3 PASS. The repository gates deliberately fail closed on a capable release runner until those facts are proven.
