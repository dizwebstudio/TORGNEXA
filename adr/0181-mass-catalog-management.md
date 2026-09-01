# ADR-0181: unified mass catalog workspace

Status: Accepted

## Context

TORGNEXA уже имеет канонический Product/PIM, Offer, карточку marketplace,
quality/publication и отдельные price/inventory контуры. Оператору нужен один
bounded workflow для нескольких channel projections, но эти projections имеют
разные taxonomy, media slots, обязательные атрибуты и capability state.

## Decision

Ввести provider-neutral `catalogbulk` как операторский слой координации. Он
фиксирует immutable selection snapshot, typed changes, before/after projection
diff, quality diagnostics, approval-bound apply intent и per-row result. PIM и
Offer не копируются и не становятся зависимыми от названия marketplace.

Операция ограничена 1 000 SKU, 8 каналами и 8 000 строками. Remote apply
partition-ится по channel/account и допускает только `qualified` target с
нужной capability. `read_only`, `partially_supported`, `ready` и
`qualification_required` отображаются отдельно и не получают ложной кнопки
записи. Preview не вызывает remote side effect; MCP также остаётся dry-run.

Lifecycle: `draft → previewed → awaiting_approval → queued → running →
partial/completed/failed/cancelled`, а каждая строка имеет собственный outcome.
Timeout после remote accept и любой неясный ответ остаются `unknown` и требуют
read-after-write/reconciliation.

## Compatibility impact

Изменение additive. Существующие PIM, Offer, listing, pricing, inventory и
publication API не меняются. Добавлены `/api/v1/catalog/bulk/*`, versioned event
payloads, Go/TypeScript/Python SDK methods и MCP dry-run descriptor. Устаревшие
клиенты продолжат работать; новые операции требуют явных permission/capability.

## Migration and data impact

Migration `000057_mass_catalog_management.sql` — expand-only. PostgreSQL хранит
bounded preview/run evidence и append-only versioned kill-switch rows с FORCE
RLS, tenant policies, idempotency uniqueness и retention-ready indexes. Эти
таблицы не заменяют Product, Offer, price или stock ledger; rollback выполняется
через capability disablement/worker drain, а evidence не удаляется.

## Security and privacy impact

Scope берётся только из authenticated organization/workspace context. Apply
требует exact approval, actor reference и idempotency key. Media принимает только
released/safe asset digest с MIME/размером/разрешением. Tokens, authorization
headers, raw provider payloads, private keys, untrusted HTML и лишний PII не
попадают в API evidence, events, logs или обычные ответы.

## Operational impact

Workspace показывает capability matrix, freshness, taxonomy/mapping version,
quality blockers, partial outcomes, remote receipt digest и reconciliation
decision. Cursor API ограничивает историю, kill switch останавливает новые
массовые записи, а queued/unknown evidence остаётся доступным для recovery.
Синтетическая qualification запускается через
`make mass-catalog-qualification`.

## Alternatives considered

Добавить channel-specific поля прямо в Product отклонено: это смешивает
каноническую товарную модель с несовместимыми схемами площадок. Выполнять bulk
из браузера без durable preview/run отклонено: нельзя доказать blast radius,
пережить crash или разобрать partial result. Считать manifest/health/SDK
достаточными для remote write отклонено: qualification должна быть подтверждена
по конкретной capability и версии evidence.

## Consequences

Контент-менеджер получает единый интерфейс для карточек, локализаций,
характеристик, вариантов, media, цен и остатков. Цена решения — реальные WB,
Ozon и другие channel writes не объявляются готовыми до credentialed
sandbox/live read-after-write evidence; до этого они остаются
`read_only`/`qualification_required`.
