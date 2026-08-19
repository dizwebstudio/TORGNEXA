# Task 033: Yandex Market connector

**Status: Repository implementation completed 2026-08-10.**

## Objective
Implement a current Connector Spec and baseline read-only assortment/prices/stocks/orders/notifications for Yandex Market; writes remain separately risk-gated.

## Dependencies
010, 014

## Acceptance
- [x] Registered read-only Yandex Market provider through Connector SDK v1.
- [x] Product, price, inventory, business-order and inbound-notification baselines.
- [x] Opaque bounded pagination, exact price decimals and explicit warehouse-mode semantics.
- [x] Secret isolation, host-mediated transport, normalized errors/rate limits and duplicate-notification dedupe.
- [x] Deterministic/adversarial fixtures plus Task-064 13-check conformance evidence.
- [x] Reconciliation mapping, capability audit, architecture evidence and repository checks.

## Boundary
Task 033 adds no marketplace write capability, no database migration and no public TORGNEXA HTTP endpoint. Future product/price/inventory/order-status writes require separate capability/risk/approval admission.
