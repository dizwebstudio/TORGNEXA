# ADR 0120: Factual channel unit economics

## Status

Accepted for Task 167.

## Context

The existing `profitability-v1` endpoint is a manual what-if calculator. It
cannot explain the actual contribution of a marketplace, web store or other
channel, and a payout must not be mistaken for a sale. Operators also need to
see incomplete cost/FX/attribution evidence instead of misleading zeroes.

## Decision

1. Add a pure, provider-neutral calculation engine with the immutable formula
   `gross - discounts - cancellations - refunds = net revenue`, followed by
   commission, payment fee, fulfilment, storage, advertising, promotion, COGS
   and penalties, plus compensation, to obtain contribution profit. Payout is
   a cash/reconciliation metric only.
2. Use one explicit recognition basis per snapshot: `order_accrual`,
   `settlement` or `cash`. The current PostgreSQL report adapter publishes
   the order-accrual view; other bases remain selectable and are fail-closed
   until their source watermarks are available.
3. Resolve channels through tenant-scoped `channel_ref` attribution. Unknown
   or ambiguous facts go to `unattributed` with a quality reason; provider
   names are never trusted as a business key.
4. Deduplicate settlement entries by source/account/provider reference and let
   settlement fees take precedence over a duplicate payment fee. Historical
   COGS is accepted only from an as-of snapshot; missing COGS is `partial`.
5. PostgreSQL stores channel mapping, cost evidence and immutable calculation
   run metadata under forced RLS. ClickHouse may project report rows but is
   disposable and never becomes a financial ledger.

## Compatibility and security

The OpenAPI report catalog is additive (`unit_economics_by_channel`) and keeps
the what-if endpoint unchanged. Amounts are integer minor units, quantities are
fixed-point strings, and all cross-tenant filters come from authenticated
scope. Reports, events and logs contain bounded references/digests only; no
credentials, provider payloads, bank details or unnecessary PII are retained.

## Operational consequences

Runs are reproducible by input digest and policy versions. Corrections create a
new run, while source ledgers remain append-only. Missing COGS, unsupported
settlement kinds, disputes and unattributed shares are visible in the API/UI.
Migration `000034_channel_unit_economics.sql` is expand-only, RLS-protected and
can be retained while readers are disabled during rollback.
