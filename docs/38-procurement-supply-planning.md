# Procurement & Supply Planning

TORGNEXA must support the inbound half of commerce, not only sales.

## Core entities

Supplier, SupplierOffer, PurchaseOrder, PurchaseOrderLine, InboundShipment, ReceivingPlan, LeadTime, ReorderPolicy, Forecast, ReplenishmentRecommendation.

## Planning inputs

- available/reserved/in-transit stock;
- sales velocity and seasonality;
- confirmed marketplace demand/signals;
- supplier lead time and MOQ;
- safety stock and target days-of-supply;
- warehouse capacity;
- promotions and advertising plans.

## Rules

Recommendations are advisory by default. Creating or sending a purchase order is a separate approved write operation. All recommendations record the input snapshot and algorithm/version that produced them.

## Task 165 foundation

Forecast and stock projection are derived planning facts, never a replacement
for the WMS inventory ledger. The current baseline is deterministic and
returns-aware: it carries the latest normalized demand forward, records an
observed upper bound, preserves stockout shortfall and applies exact decimal
MOQ/case-pack rounding. Every run stores an input digest, algorithm version and
quality status in PostgreSQL with tenant RLS.

The supported policy modes are `recommendation_only`, `draft_po` and a guarded
`auto_submit` allowlist. Only the first mode is active in the foundation slice;
draft creation, supplier execution, durable scheduling and live qualification
must pass the existing procurement, approval, capability and reconciliation
boundaries before production enablement.
