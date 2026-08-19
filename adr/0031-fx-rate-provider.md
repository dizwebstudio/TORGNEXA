# ADR 0031: Versioned FX Rates Through Provider Port

## Decision
All cross-currency conversions use immutable sourced FXRate records and explicit rounding/triangulation policy. Current and historical providers are pluggable.

## Consequences
Financial reports are reproducible and do not silently change when market rates move.
