# ADR 0065: Durable historical FX resolution and conversion

## Status
Accepted — Task 089b.

## Context
Task 089a froze exact immutable FX facts, source precedence, rounding and triangulation policy but deliberately disabled executable conversion. Reporting, marketplace settlement and payment reconciliation now need reproducible historical cross-currency derivation without binary floating point, mutable current-rate tables or provider leakage into finance domains.

## Decision
1. Store FX observations as append-only global reference facts keyed by immutable fact ID. Persist separate append-only resolution and conversion evidence.
2. Resolve only an explicit ordered pair/rate type/as-of. Source precedence is deterministic and every configured source has an explicit maximum effective age. Missing/stale rates fail closed.
3. Cache only persisted fact IDs. Cache is acceleration, never authority.
4. Extend Connector SDK v1 additively with capability-specific `FXRateReader`; root connector/runtime interfaces remain unchanged. External providers import only the Connector SDK.
5. Admit `cbr-fx` as the first reference adapter. The adapter reads an explicitly dated official Bank of Russia daily quotation and never performs implicit inversion.
6. Perform conversion with integer/fixed-decimal arithmetic through `big.Int`, no binary float and no intermediate rounding. Round once at the declared target minor-unit scale using an explicit policy.
7. Reporting and reconciliation may cross currencies only through a converter that returns the persisted conversion-record ID. Existing same-currency paths remain compatible.

## Alternatives considered
- Mutable `current_rate` rows: rejected because historical results would change after rate refresh.
- Storing only decimal+source name on finance rows: rejected because precedence, exact fact identity and arithmetic policy would be unrecoverable.
- Binary floating point: rejected because money derivation must be exact and deterministic.
- Implicit inversion/arbitrary graph search: rejected because it invents unreviewed route semantics and can hide missing data.
- Provider-specific CBR logic in finance/reporting: rejected; external source semantics stay behind Connector SDK and a host adapter.

## Compatibility impact
All changes are additive. Existing FX v1 contracts and same-currency reporting/reconciliation continue unchanged. Connector SDK root interfaces remain v1-compatible; `FXRateReader` is a capability-specific additive interface. Existing provider IDs do not enter Core or provider-neutral finance packages.

## Migration and data impact
Migration `000040` adds `fx_rate_facts`, `fx_resolution_evidence` and `fx_conversion_records` as global append-only reference/evidence tables. No existing table is rewritten and there is no destructive backfill. Historical consumers retain original money and reference immutable conversion records rather than mutating source facts.

## Operational impact
Deployment must bind the typed CBR transport to the official source, configure source precedence/freshness and a reviewed currency minor-unit registry, and retain FX fact/resolution/conversion tables with finance backups. Cache can be dropped/rebuilt at any time because it contains persisted fact IDs only. Missing/stale rates block cross-currency derivation rather than using a stale fallback.

## Security and privacy impact
The initial source is read-only/no-auth. Provider code has no SQL/Core/filesystem/process/direct-network authority; egress remains host-owned. Reference facts contain no tenant PII. Source references are bounded opaque strings without credentials/query strings. Financial consumers receive only target money plus immutable conversion-record IDs.

## Consequences
Cross-currency finance is executable and historically reproducible only for routes with available fresh explicit facts. The first CBR adapter naturally covers foreign-currency/RUB official pairs; unsupported inverse/cross pairs fail explicitly. Additional sources require separate Connector SDK conformance and architecture review.
