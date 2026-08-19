# Settlement / Payment Reconciliation

Task `059` implementation lives in `internal/platform/paymentreconciliation`; Task `089b` adds the qualified historical FX bridge.

## Safety invariants

Reconciliation classifies differences instead of rewriting financial facts. `Reconcile` remains same-currency and fail-closed. `ReconcileWithFX` may compare a settlement in another currency only through a historical converter that returns an immutable persisted conversion-record reference. Used references are returned in `Report.FXConversionRefs`. Missing/stale rates, an invalid target currency, or a conversion without evidence fail explicitly.

The settlement ledger still preserves the provider's original amount and currency; reconciliation never rewrites it.

## Persistence

PostgreSQL expand migrations are `000038_settlement_payment_reconciliation.sql` plus Task-089b global append-only FX evidence in `000040_fx_rate_provider_completion.sql`. In-memory implementations in tests are reference semantics, not production durability.
