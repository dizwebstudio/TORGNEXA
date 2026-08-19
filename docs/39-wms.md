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
