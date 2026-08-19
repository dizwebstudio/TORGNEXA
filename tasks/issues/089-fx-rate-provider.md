# Task 089: Fx Rate Provider

## Status

Stage `089a` repository implementation: **Completed** on 2026-08-09. Stage `089b` repository implementation: **Completed** on 2026-08-12. Parent Task `089`: **Repository-complete**.

Stage `089a` established the immutable boundary. Stage `089b` now supplies append-only historical storage, Connector-SDK reference-source admission, explicit freshness/cache policy, deterministic resolution evidence and verified exact conversion.

## Objective
Implement versioned FXRate domain/provider port with historical rates, source precedence, fixed-decimal conversion/rounding and reproducible financial snapshots.

## Dependencies
- Stage `089a`: `076`.
- Stage `089b`: `049`, `058`, `059`, and completed `089a`.

## Deliverables
Parent Task `089` delivers the rate model/storage/API/provider interface, reference adapter candidate (for example a central-bank source), caching/staleness/reconciliation and tests. Stage `089a` deliberately delivers only the immutable model/provider/policy/snapshot boundary; stage `089b` delivers persistence and executable conversion.

## Stage 089a — immutable contract/provider foundation

- [x] Immutable `RateFact` with exact positive Decimal, ordered currency pair, source/reference, rate type, UTC observed/effective instants, and schema version.
- [x] Provider port requires an explicit UTC `as_of` lookup and returns a full immutable fact rather than a naked decimal.
- [x] Deterministic source precedence selects only exact-pair/rate-type applicable facts and fails explicitly on missing data; it never inverts or triangulates implicitly.
- [x] Rounding policy is explicit (`half_even|half_up|toward_zero`) and fixed to final-output-only rounding.
- [x] Triangulation policy is either direct-only or one explicit pivot; arbitrary graph search/intermediate rounding are forbidden.
- [x] Reproducible conversion-snapshot contract requires original/result Money, ordered full rate facts, rounding/triangulation policy, target minor-unit scale and UTC derivation time.
- [x] Additive Draft 2020-12 contracts and `finance.fx_rate.published.v2` preserve the older v1 contracts unchanged.
- [x] No database migration, cache, provider adapter or conversion implementation is introduced in `089a`.
- [x] Architecture governance supports a future append-only `ARCH-089B` supplemental review so stage `089b` can change protected storage/adapter paths without mutating `ARCH-089`.

## Stage 089b — repository completion

- [x] Immutable storage and historical query adapter.
- [x] Reference FX source adapter behind Connector SDK/conformance.
- [x] Cache and explicit staleness policy with fail-closed missing/stale errors.
- [x] Source reconciliation and precedence evidence against persisted facts.
- [x] Exact conversion arithmetic with explicit target-currency minor units and verified snapshot arithmetic.
- [x] Historical reproducibility across reporting/settlement/payment reconciliation; closes parent Task `089`.

## Acceptance
No binary float; financial derivation records rate/source; historical reports are reproducible; missing/stale rates fail explicitly.

Run required repository checks and report results, risks and follow-ups.
