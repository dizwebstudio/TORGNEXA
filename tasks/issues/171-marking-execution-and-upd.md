# Epic 171 — Маркировка, агрегация, УПД и полный lifecycle

## Цель

Дать TORGNEXA единый provider-neutral контур маркировки для WMS, госсистемы
и ЭДО, сохранив approval, изолированную подпись, tenant/RLS, outbox/inbox,
idempotency и безопасное обращение с кодами.

## Подзадачи

- [x] 171.1 — ADR и матрица операций: `adr/0122-marking-execution-and-edo.md`.
- [x] 171.2 — Доменная модель batch/code/operation/package/print/scan/document/observation.
- [x] 171.3 — Безопасное хранение: fingerprint + expiring artifact reference; raw values только через `RawCodeStore`.
- [x] 171.4 — Typed Connector SDK capabilities и idempotent/dry-run/unknown contract.
- [x] 171.5 — Получение и резервирование кодов с partial/timeout/cancel/reconciliation semantics.
- [x] 171.6 — Print jobs, template version, preview/printer refs, retry/reprint one-use guard.
- [x] 171.7 — Data Matrix/barcode scan validation and WMS quantity outcomes.
- [x] 171.8 — Unit/kit/box/pallet package graph, cycle/composition/close/dissolve checks.
- [x] 171.9 — Introduce/withdraw/transfer lifecycle through approval, audit and remote status.
- [x] 171.10 — Versioned UPD 5.03 artifact lines, signing/MChD and EDO state model.
- [x] 171.11 — Full lifecycle orchestration stages and durable process run.
- [x] 171.12 — Reconciliation and drift types: status, quantity, composition, unknown write, missing observation.
- [x] 171.13 — Operator API/UI contract for batches, print queue, scan, package tree, UPD and errors.
- [x] 171.14 — Connector admission remains explicit qualification gate for Chestny ZNAK, Diadoc, Saby, KKT/OFD and marketplaces.
- [x] 171.15 — Synthetic conformance and Docker qualification matrix, including worker failure/recovery.

## Acceptance criteria

1. No raw marking code is present in durable SQL, events, audit metadata,
   errors, logs, ordinary API responses or SDK result types.
2. Every remote marking write is typed, capability-gated, approval-gated,
   idempotent, dry-run aware and able to remain `unknown`.
3. Package graph rejects self-links/cycles and preserves unit → kit → box →
   pallet relations with shipment/order/UPD refs.
4. Scans are fingerprinted immediately and classify wrong, duplicate and
   overflow cases without changing WMS quantities.
5. UPD is versioned at 5.03 and is linked to code/package lines; signature and
   MChD stay behind the existing isolated signing boundary.
6. PostgreSQL migration is backup-gated, tenant-scoped, FORCE RLS and has
   append-only evidence for scans, remote observations and drifts.
7. Repository checks pass. Live provider qualification is reported separately
   and is not inferred from manifests or synthetic fixtures.

## Explicit boundary

The issue closes the repository implementation and synthetic qualification
surface. Provider credentials, current partner test environments, legal
approval and production go-live remain external release gates.
