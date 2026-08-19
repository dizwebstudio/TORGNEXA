# Task 074 — Logistics carrier SDK

Connector SDK v1 gains rate, shipment-create/cancel, label, tracking and return ports. Addresses, parcel dimensions/weight, exact costs and normalized delivery windows are provider-neutral. Carrier tariff/service codes remain provider mappings.

`internal/platform/logistics` ranks routing options deterministically by comparable same-currency cost and SLA. Remote tracking remains authoritative; Task 090 will prove the abstraction with a production reference carrier.
