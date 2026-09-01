# WMS Core

WMS is an optional-but-native module for sellers operating their own warehouses and PUDO network.

## Capabilities

- receiving and discrepancy registration;
- locations/bins/zones;
- put-away;
- stock ledger and reservations;
- lots, serials, GTIN/DataMatrix references, expiry dates;
- picking waves and tasks;
- packing, labels and shipment handoff;
- cycle count/full inventory;
- damaged/quarantine stock;
- transfers between warehouses/PUDO;
- returns receiving and disposition.

## Invariants

Inventory changes are ledgered, idempotent and attributable to a business document/event. `available` is derived from physical/on-hand, reserved, quarantine and allocation state; clients must not overwrite it as an arbitrary scalar.

WMS events integrate with Kafka, Sync/Reconciliation, ChZ marking and Fulfillment.

## Current repository slice

The durable operator surface is implemented in `wms_execution_tasks` and
`wms_execution_task_events` (migration `000030`). It supports queue reads,
claim/start/scan/complete/exception/cancel, order-to-pick creation and
standalone `receiving`, `put_away` and `cycle_count` tasks. Scans retain only a
SHA-256 barcode digest plus location, exact quantity, actor and UTC time.

Migration `000031` adds an internal `pack_handoff` batch for up to 50 completed
pick tasks from one warehouse. The batch is visible and auditable through the
WMS API and can be handed off to a local pack area. Marketplace orders write,
labels, Честный знак, shipment confirmation and automatic on-hand consumption
are not claimed by this slice.

## Mobile warehouse workspace (Task 229)

The mobile surface at `/warehouse/mobile` is a projection over the same WMS
tasks; it is not a second inventory ledger. It provides bounded pick batching,
digest-only barcode/GTIN/DataMatrix scan evidence, exact grams/mm package facts,
host-owned print jobs, registered devices and offline reconnect receipts.

FBS uses the local `pick → scan → pack → print → handoff` path. FBO is
marketplace-owned: the UI exposes inbound/order visibility and remote
observations, while local pick/pack/print is blocked. Hybrid and split plans
keep seller-owned and remote-owned work separate.

The API is under `/api/v1/warehouse-mobile`; all writes are tenant-scoped,
idempotent and version-checked. See
`docs/operations/229-mobile-warehouse-fulfillment.md` for recovery and
qualification rules. Repository qualification does not replace credentialed
marketplace or scanner/scale/printer evidence.
