# Repository validation report — P4 + Tasks 119–121 — 2026-08-18

## Inventory

- repository-implemented tasks: `001`–`121`;
- architecture: **117 modules / 32 provider modules / 112 reviews**;
- active PostgreSQL baseline: **11 migrations**, latest `000011`; archived pre-v1 lineage: **74 migrations**, legacy head `000074`;
- public OpenAPI/generated SDK surface: **108 operations / OpenAPI 0.15.0**;
- connector catalog: **32 connectors**;
- repository license: **Apache-2.0**.


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
