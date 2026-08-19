# Task 016

**Status: Repository implementation completed 2026-08-10.**

Products/stocks/orders read baseline via official JSON API, pagination/throttling/fixtures.

## Acceptance
- [x] Read-only MoySklad provider registered through Connector SDK v1.
- [x] Product, stock-by-store and customer-order read baselines.
- [x] Bounded opaque pagination and mid-row inventory resume.
- [x] Bearer secret isolation, host-mediated transport, gzip intent and conservative throttling.
- [x] Deterministic/adversarial fixtures and Task-064 conformance evidence.
- [x] Updated capability contract/docs/architecture evidence and required repository checks.

## Boundary
Task 016 introduces no MoySklad write capability, no migration, no public TORGNEXA API and no provider identifier in Core.
