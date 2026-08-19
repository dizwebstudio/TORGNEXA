# Marketplace Settlements & Financial Ledger

Sales reporting is insufficient without settlement reconciliation.

## Normalized ledger

SettlementEntry records marketplace/acquirer/bank facts: sale, commission, logistics, storage, advertising, penalty, refund, compensation, withholding, payout and adjustment.

Every entry has provider reference, period, currency, gross/net amount, tax metadata where relevant and links to orders/returns/campaigns when resolvable.

## Reconciliation

Expected operational economics are compared to provider settlement reports and actual bank receipts. Differences are classified as known fee, timing difference, unmatched, duplicate or disputed.

The ledger is append-oriented; corrections are adjustments, not destructive rewrites. See `contracts/ledger/settlement-entry.schema.json`.
