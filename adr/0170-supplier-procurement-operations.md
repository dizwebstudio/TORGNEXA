# ADR-0170 — Supplier & Procurement Operations

Status: Accepted

## Context

В TORGNEXA уже есть базовые таблицы и lifecycle `PurchaseOrder` из Task 052,
рекомендации пополнения, канонический `LegalParty`, WMS ledger, approval/audit
и upload security pipeline. Нужен пользовательский контур закупок вокруг этих
источников истины, без второго PO-домена, копирования юридических реквизитов и
прямого изменения остатков.

## Decision

Epic 173 реализуется как additive procurement workbench:

- supplier profile хранит только ссылку `LegalPartyID`, операционные контакты,
  условия, валюту, срок и минимальную сумму заказа;
- supplier offer хранит supplier SKU/GTIN, canonical offer reference, цену,
  MOQ, case pack, lead time, priority и период действия; каждое изменение
  оставляет append-only price evidence;
- CSV/XLSX принимается только после release upload pipeline. Сервис проверяет
  digest, строит preview, сопоставляет GTIN затем supplier SKU, а ручное
  сопоставление разрешает только конкретный canonical offer. Неоднозначная
  строка не применяется молча;
- draft PO использует существующий `PurchaseOrder` state machine. Рекомендация
  привязывается к digest исходного replenishment snapshot и отклоняется при
  устаревшем digest;
- approve/send требует matching approved request, а remote/export timeout
  сохраняется как `unknown` и доступен для reconciliation/retry;
- receiving записывает факт в tenant-scoped append-only table и публикует
  событие для WMS. Procurement не пишет inventory ledger напрямую;
- PostgreSQL остаётся operational truth, все прикладные записи проходят
  tenant RLS, audit и transactional outbox. Внешние provider calls будут
  добавлены только через connector capability qualification.

## Compatibility impact

OpenAPI и SDK расширяются аддитивно. Existing Task 052 readers and lifecycle
remain valid. New permissions are `procurement.*`; manager/operator roles may
operate them, viewer role is read-only. No provider name is introduced in Core.

## Migration and data impact

Migration 45 is expand-only and requires a verified backup. It adds procurement
columns, scoped indexes, price-list previews and append-only offer history,
PO events, receiving facts and reconciliation findings. Raw uploaded bytes,
tokens and provider payloads are not stored in these tables.

## Security and privacy impact

The API derives organization/workspace from authenticated context and every
repository query sets PostgreSQL tenant settings. Upload import accepts only a
released object and verifies its SHA-256. Audit/event summaries contain IDs,
states and bounded counts, never credentials, raw price-list content or
unnecessary personal data. Legally significant send/EDO actions remain behind
approval and separate connector qualification.

## Operational impact

Operators get a single procurement workbench with supplier directory, current
offers, import preview/commit, draft PO queue and reconciliation attention.
Stale recommendations, duplicate idempotency keys, quantity overflow and
ambiguous external outcomes fail closed. Rollback is capability disablement and
worker drain; no destructive down migration is provided.

## Alternatives considered

Duplicating LegalParty in a supplier table was rejected because identity and
legal documents must remain canonical. A second procurement PO state machine
was rejected because it would split lifecycle and audit semantics. Applying a
price list directly during upload was rejected because operators need a
reviewable preview and ambiguous rows must never mutate offers. Direct stock
updates on receiving were rejected because WMS owns inventory ledger facts.

## Deferred qualification

Chestny ZNAK, Diadoc, Saby EDO, KKT/OFD and marketplace orders/supply writes
remain explicit follow-up boundaries. Their official API contracts, signing,
MChD, retries, remote observations and conformance fixtures must be admitted
separately before runtime capability is enabled.

## Consequences

The operator can maintain supplier conditions and prepare auditable purchase
orders without copying legal identity or bypassing WMS. Price imports become a
reviewable two-step operation, and remote uncertainty is visible instead of
being reported as success. The platform does not yet claim automatic EDO,
marking, fiscal or marketplace-order writes; those integrations require their
own official API and conformance evidence.
