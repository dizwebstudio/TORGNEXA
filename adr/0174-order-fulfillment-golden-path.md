# ADR-0174: Order fulfillment golden path

Status: Accepted

## Decision

Закрыть Task 224 одним provider-neutral orchestration projection поверх
канонических доменов:

```text
order → reservation → pick_pack → label → shipment → return → refund
→ settlement → reconciliation
```

`marketplaceoperations.Flow` не становится вторым Order, Inventory, WMS,
Shipment, Return, Payment или Ledger. Он хранит только tenant-scoped ссылки,
состояние стадии и append-only idempotency command journal. Стадия `label`
явно отделена от `shipment`, чтобы timeout при получении этикетки не выдавался
за принятую перевозчиком отгрузку.

## Invariants

- Существующий canonical importer проверяет mapping, валюту, exact money,
  decimal quantity, tax snapshot и дедупликацию до запуска reservation.
- Reservation/WMS/returns/payment repositories остаются владельцами своих
  фактов; flow принимает только ссылку и нормализованный outcome.
- Повтор команды с тем же payload идемпотентен; изменённый payload с тем же
  ключом — конфликт.
- `unknown` останавливает последовательность и требует status read или
  reconciliation. Автоматический слепой повтор внешнего write запрещён.
- API и UI показывают только redacted references, outcomes и bounded reason
  codes. Credentials, raw payloads, DataMatrix/barcode values и payment data
  не сохраняются.

## Compatibility and migration

OpenAPI расширен additive endpoint-ом flow detail и параметрами запуска с
канонического заказа. Миграция `000051_marketplace_order_fulfillment.sql`
расширяет stage check для `label`; она требует backup и не меняет бизнес-факты.
Существующие flows продолжают работать с прежними стадиями.

## Release boundary

Repository synthetic qualification и external connector qualification — разные
gates. Production claim разрешён только после сохранённого evidence на
официальном marketplace, carrier и payment/fiscal sandbox/live контуре. При
отсутствии credentials runtime остаётся fail-closed.
