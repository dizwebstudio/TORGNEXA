# Epic 173: Supplier & Procurement Operations

## Status

`repository-complete` — 2026-08-31.

Repository task key: `218`. The user-facing Epic 173 number is already used by
the existing PЭК task `tasks/issues/173-pek-bounded-shipment-create.md`, so
this implementation uses the next free repository key and keeps `173` in the
title, ADR and product documentation. Duplicate task IDs are not introduced.

## Objective

Дать оператору единый контур закупок: канонический поставщик, офферы и
история цен, безопасный импорт прайс-листа, заказ поставщику из рекомендации,
approval/send, приёмка через WMS и reconciliation.

## Deliverables

- [x] 173.1 — ADR, границы контуров и матрица lifecycle.
- [x] 173.2 — Supplier profile API/UI с ссылкой на canonical LegalParty.
- [x] 173.3 — SupplierOffer с MOQ, case pack, lead time, priority и append-only history.
- [x] 173.4 — CSV/XLSX preview/commit только из released `ReleasedObjectRef`.
- [x] 173.5 — Безопасное сопоставление GTIN → supplier SKU → ручной mapping; ambiguous rows остаются unresolved.
- [x] 173.6 — PurchaseOrder workbench поверх существующего Task 052 lifecycle.
- [x] 173.7 — Draft PO из рекомендации с digest immutable replenishment snapshot и stale check.
- [x] 173.8 — Approval-bound approve/send, idempotency, retry и explicit unknown outcome.
- [x] 173.9 — Receiving facts с проверкой количества и связью с WMS; прямой stock mutation запрещён.
- [x] 173.10 — Операторский раздел «Закупки» для поставщиков, офферов, прайс-листов, PO и внимания.
- [x] 173.11 — Additive OpenAPI и сгенерированные Go/Python/TypeScript SDK.
- [x] 173.12 — Outbox, audit, append-only PO/receipt evidence и reconciliation findings.
- [x] 173.13 — Deterministic tests, migration/catalog checks, Docker qualification.
- [x] 173.14 — Connector boundary зафиксирован: Chestny ZNAK, Diadoc, Saby EDO, KKT/OFD и marketplace orders подключаются только после отдельной qualification.

## Acceptance boundary

Контур пригоден для ручного supplier/procurement workflow и синтетической
qualification. Поставщики ссылаются на существующий `LegalParty`, а не
дублируют юридические реквизиты. Цены импортируются через preview и не
применяются без явного commit. PO проходит существующий lifecycle; approval
проверяется по тому же resource ID, отправка имеет состояния `sent` и
`unknown`, а приёмка создаёт durable факт для WMS, но не редактирует inventory
ledger напрямую.

Внешние юридически значимые операции (ГИС МТ, УПД/ЭДО, ККТ/ОФД и реальные
marketplace-заказы) не объявляются готовыми этим эпиком: для них сохранён
fail-closed connector contract и отдельный qualification scope.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`, contract/migration/architecture
checks, generated SDK checks, frontend typecheck/build and `git diff --check`.
Live supplier credentials and uploaded source files are not required and must
never be committed.
