# Repository validation report — Tasks through 132 — 2026-08-27

## Inventory

- repository-implemented tasks: `001`–`132`;
- architecture: **125 modules / 38 provider modules / 123 reviews**;
- active PostgreSQL baseline: **17 migrations**, latest `000017`; archived pre-v1 lineage: **74 migrations**, legacy head `000074`;
- public OpenAPI/generated SDK surface: **134 operations / OpenAPI 0.21.1**;
- connector catalog: **38 manifests / 11 generic runtime integrations / 8 working providers on separate surfaces / 19 planned**;
- repository license: **Apache-2.0**.

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
