# Task 034: Megamarket connector

**Status: Repository implementation completed 2026-08-10.**

## Objective
Audit current official/partner interfaces and implement only supported baseline catalog/order/stock capabilities behind MarketplaceConnector.

## Dependencies
010, 014

## Acceptance
- [x] Current official Megamarket merchant API/auth/scheme capability audit; no scraping.
- [x] Registered read-only `megamarket` provider through Connector SDK v1.
- [x] Product catalog, configured-warehouse stock, and order-search read baselines.
- [x] Opaque bounded pagination, secret isolation, host-mediated transport and normalized remote failures.
- [x] Deterministic/adversarial fixtures and Task-064 13-check conformance evidence.
- [x] Reconciliation mapping, provider spec, architecture evidence and repository checks.

## Boundary
Task 034 grants no marketplace write capability, adds no database migration and adds no public TORGNEXA HTTP endpoint. Price read/write, stock writes, product writes, order-status writes and additional scheme-specific operations require separate admission.
