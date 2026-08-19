# TORGNEXA handoff — P4 + Enterprise UX + pre-v1 migration baseline

Date: 2026-08-18

## Current state

Tasks `001`–`121` are repository-implemented. Task 118 adds the fail-closed P4 go-live evidence/publication layer; Task 119 closes the base operator UI/UX gap; Task 120 upgrades enterprise operations with server-owned grids, realtime invalidation, unified incidents, deep/server search and reporting-backed analytics; Task 121 replaces the 74-file development migration install path with an 11-file pre-v1 baseline and verified legacy rebaseline.

Current repository inventory:

- architecture: **117 registered modules / 32 provider modules / 112 reviews**;
- active PostgreSQL baseline: **11 migrations**, latest `000011_runtime_operations.sql`; archived immutable pre-v1 lineage: **74 migrations**, legacy head `000074_fulfillment_failover_execution.sql`;
- public OpenAPI/generated SDK surface: **108 operations / OpenAPI 0.15.0**;
- connector catalog: **32 connectors**;
- repository license: **Apache-2.0**.

Repository completion is still distinct from release-topology qualification. Task 117 makes runtime qualification mandatory in the release workflow, but this source archive cannot manufacture Docker, OIDC, GitHub Ruleset or live provider evidence.



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
- migration catalog/baseline: **PASS — 11 active migrations / latest 000011; legacy 74-file head pinned and archived**;
- architecture policy: **PASS — 117 modules / 32 providers / 109 reviews**;
- generated public SDKs: **PASS — 108 operations / OpenAPI 0.15.0**;
- frontend shell/catalog/static policy: **PASS — 18/18 / 32 connectors**;
- JS supply-chain repository/lock and Community deployment policies: **PASS**;
- release and required-workflow YAML/P4 static invariants: **PASS**;
- all new P4 shell/Python source syntax checks: **PASS**.

The host still exposes Go 1.23.2 and has no Docker command/network access to fetch the pinned Go 1.26.5 toolchain or missing modules. Therefore this handoff does not claim a fresh full-tree Go 1.26.5 PASS or deployment-level P3 PASS. The repository gates deliberately fail closed on a capable release runner until those facts are proven.
