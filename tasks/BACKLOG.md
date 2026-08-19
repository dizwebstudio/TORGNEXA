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
  has regressed to 22 High plus 1 Critical (new Go stdlib CVEs since the last
  snapshot); Kafka is unchanged at 10 High. Remediation is still outstanding.
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
