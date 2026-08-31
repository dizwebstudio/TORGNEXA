# Epic 172: Marketplace Product Publication

## Status

`repository-complete` — 2026-08-31.

Repository task key: `217`. The numeric key is different because `tasks/issues/172-yandex-market-inventory-write.md` is an existing canonical task and the architecture checker requires task IDs to be unique. This document closes the user-facing Epic 172 scope.

## Objective

Публиковать товары на marketplace через проверенный versioned publication
snapshot, с approval, идемпотентностью, асинхронными статусами,
reconciliation и операторским контролем.

## Deliverables

- [x] 172.1 — ADR и capability matrix для WB, Ozon, Yandex Market и denied/deferred providers.
- [x] 172.2 — provider-neutral snapshot без marketplace-полей в Core Product.
- [x] 172.3 — typed Connector SDK с dry-run, idempotency и normalized outcomes.
- [x] 172.4 — tenant/account scoped idempotent queue и безопасное восстановление после timeout.
- [x] 172.5 — snapshot validation для SKU, GTIN, category, attributes, dimensions, price и locale.
- [x] 172.6 — ReleasedObjectRef-only media boundary, digest checks и запрет внешних URL.
- [x] 172.7 — Product Quality receipt перед каждым live write.
- [x] 172.8 — durable PostgreSQL queue/worker с CAS transitions, bounded claim и retry-safe mapping.
- [x] 172.9 — раздельные local/remote/moderation/operation states и normalized receipts.
- [x] 172.10 — WB create/update adapter и bounded status read.
- [x] 172.11 — Ozon import/status adapter с async task mapping.
- [x] 172.12 — Yandex Market offer-mapping update и bounded status read.
- [x] 172.13 — explicit runtime denial для не квалифицированных Megamarket, Magnit Market, AliExpress RU, Lamoda и М.Видео.
- [x] 172.14 — append-only observations/drifts с заявленными drift types.
- [x] 172.15 — preflight, enqueue, list, detail, retry API и operator publication screen.
- [x] 172.16 — deterministic connector/API/core tests, migration/catalog checks и Docker qualification gate.

## Acceptance boundary

WB, Ozon и Yandex Market имеют admission только для тех операций, которые
подтверждены текущими официальными API-контрактами и локальными fixtures.
Неподключённые media/attribute bridges и остальные providers не маскируются
под успешную публикацию: адаптеры возвращают нормализованный unsupported,
worker переводит операцию в `needs_attention`, а capability остаётся
fail-closed до отдельной квалификации.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`, `./scripts/check-contracts.sh`,
`./scripts/check-migrations.sh`, `make architecture`, SDK/frontend checks and
`git diff --check`. Live provider credentials are not required for deterministic
qualification and must never be committed.
