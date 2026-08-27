# FX / Exchange Rate Provider

Task `089` is complete after stages `089a` and `089b`. Money remains integer minor units/fixed decimals; every cross-currency derivation is backed by immutable sourced rate facts, deterministic resolution evidence and an immutable conversion record.

## Immutable historical facts

`RateFact` records a stable ID, ordered base/quote currencies, positive exact Decimal rate, source/reference, semantic rate type and UTC observed/effective times. Facts are append-only. `USD/RUB` and `RUB/USD` are different facts; inversion is never implicit.

Migration `000040_fx_rate_provider_completion.sql` adds global reference tables `fx_rate_facts`, `fx_resolution_evidence` and `fx_conversion_records`. UPDATE/DELETE are rejected at database level. These tables contain reference/derivation data rather than tenant PII; tenant-owned finance entities keep their own scope and reference conversion IDs for lineage.

## Connector SDK and reference source

Connector SDK v1 has an additive capability-specific `FXRateReader`; the frozen root `Connector` and `Runtime` interfaces are unchanged. Provider code returns `FXRateObservation`. A host-side `fx.ConnectorProvider` validates and converts the observation into the immutable domain fact before persistence.

`cbr-fx` is the first admitted reference provider. Its host transport binds to the official Bank of Russia daily XML endpoint with an explicit requested date. It emits official foreign-currency/RUB observations and does not synthesize inverses or hidden cross rates.

Task 131 composes that transport in the production worker. The worker refreshes
the reviewed CBR currency set immediately after startup and every six hours.
One dated XML document is cached in process for 15 minutes to avoid downloading
the same table for each pair; only immutable PostgreSQL facts and resolution
evidence are authoritative. A CBR outage is logged and retried without stopping
unrelated worker components. The configured freshness ceiling is 14 days, after
which consumers fail closed rather than treating retained history as current.
The admitted set currently contains 53 exact pairs. IRR/RUB is deliberately
excluded because the official `Value / Nominal` result requires decimal scale
10 while the canonical exact-decimal contract permits at most scale 9; the
worker does not round a source fact to make it fit.

## Resolution, precedence and staleness

`Resolver.Resolve` first checks immutable storage, then configured providers in explicit `SourcePrecedence`. Every source has a reviewed `FreshnessPolicy`. Provider results are persisted before they can be selected. Selection evidence records candidate IDs, precedence and the selected fact.

The in-memory cache contains only persisted fact IDs. Every cache hit is reloaded from storage and freshness-checked. Missing data returns `ErrRateMissing`; a rate outside its configured window returns `ErrRateStale`. There is no stale fallback.

## Exact conversion

`Resolver.Convert` requires source Money and source minor-unit scale, target currency and target minor-unit scale, explicit UTC `as_of`, rate type, rounding and triangulation policies. Arithmetic uses integer coefficients and `big.Int`; no `float32/float64` is accepted and there is no intermediate rounding. The declared rounding (`half_even`, `half_up`, `toward_zero`) is applied once to the final target minor units.

After calculation the engine independently verifies snapshot arithmetic, then appends `ConversionRecord` containing the complete snapshot, source scale, resolution-evidence IDs and digest. A repeated business derivation uses the same stable conversion ID; conflicting immutable content is rejected.

`FinancialConverter` is the narrow consumer bridge. Its minor-unit registry is copied at construction and therefore cannot drift during the lifetime of a historical derivation.

## Reporting / settlements / payment reconciliation

ClickHouse continues to store raw analytical money in original currency. `reporting.ConvertSalesBucket` converts only a caller-selected bucket at an explicit UTC as-of and returns `FXConversionRecordID`; a cross-currency aggregate without this evidence remains forbidden.

The settlement ledger remains append-only and preserves original provider amount/currency. `FXRateRef` is lineage only; it never rewrites the source amount.

`paymentreconciliation.Reconcile` remains the backward-compatible same-currency path. `ReconcileWithFX` accepts a narrow historical converter and records every used conversion ID in `Report.FXConversionRefs`. Missing/stale conversion fails explicitly instead of being classified as a successful match.

## Operational rules

- reference source transport is host-owned and egress-qualified;
- source precedence and freshness are configuration/policy, not provider defaults;
- no mutable `current_rate` table exists;
- cache is disposable and can be rebuilt from facts;
- restoring historical finance requires FX fact/resolution/conversion tables together with the finance records that reference them;
- adding another source requires Connector SDK conformance and an architecture review, not a provider name in Core.
- the Integration catalog links CBR FX to Finance rather than offering a fake tenant account or product synchronization policy.
