# ADR-0180: Мобильный складской контур и FBS/FBO ownership

Status: Accepted

## Контекст

WMS, allocations, order fulfillment и logistics уже являются каноническими
bounded contexts. Оператору не хватало единого узкого интерфейса для handheld
устройства: pick-list, scan, pack, печать, offline reconnect и понятное
разделение локальной FBS-работы и удалённых FBO-фактов.

## Решение

Task 229 добавляет tenant-scoped mobile projection поверх существующих WMS и
fulfillment records. `FulfillmentMode` принимает `fbs`, `fbo`, `hybrid` и
`split`; `owner` фиксирует `seller_warehouse`, `marketplace` или `carrier`.
Mobile plan не является inventory/order/shipment ledger и не создаёт резерв:
pick batch только группирует уже созданные WMS pick tasks.

Для FBS и seller-owned части hybrid/split разрешены локальные pick, pack,
print и handoff. FBO имеет owner `marketplace`, локальная execution запрещена;
доступны только remote visibility, tracking и сохранение provider-authoritative
observation. Remote status не считается локальным завершением без receipt.

В handheld API raw barcode/DataMatrix существует только в транзитной команде.
В Postgres, outbox, audit и ответах сохраняется только SHA-256 digest и
минимальные operational facts. Offline queue хранит reconnect receipts и
payload digest; on-hand, reservation, shipment, refund и remote status нельзя
утвердить offline. Idempotency-Key, expected version, device state и tenant
scope обязательны.

Print job означает постановку намерения в host-owned queue, а не физическое
подтверждение принтера. `unknown` требует receipt/reconciliation. Reprint
отдельно аудируется и не создаёт новый shipment.

## Совместимость и данные

Migration `000056_mobile_warehouse_fulfillment.sql` — expand-only, с backup
gate, catalog hash, forced RLS, tenant policies и append-only scan/observation
history. Существующие WMS task/ledger, allocation, shipment и order tables не
переписываются. События `mobile_task_changed`, `mobile_scan_recorded` и
`mobile_print_job_changed` публикуются через Transactional Outbox.

## Безопасность и эксплуатация

Устройства регистрируются на warehouse scope и могут быть немедленно revoked.
Permission split использует `stock.read` и `wms.write`; printer/scanner/scale
credentials не входят в mobile projection. Ошибки, version conflicts, device
revoke, printer outage и unknown handoff остаются видимыми оператору.

## Qualification boundary

`make mobile-warehouse-qualification` проверяет repository-контур, contracts,
SDK, migration, RLS, redaction и frontend wiring. Credentialed sandbox/live
проверки FBS/FBO connector, scanner, scale, printer и carrier должны пройти на
целевой topology с redacted evidence; до этого capability остаётся
`read_only`, `partially_supported` или `qualification_required`.
