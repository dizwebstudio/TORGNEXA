# ADR 0029: Maintain a Production Reference Acquirer

## Decision
Prove PaymentProvider with SBP plus one card/acquiring reference connector chosen by current API audit. No raw card credentials enter TORGNEXA.

## Consequences
Payment SDK/conformance covers payment, webhook, refund and reconciliation realities before more providers are added.
