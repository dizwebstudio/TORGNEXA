# ADR 0069: Provider-neutral KKT and OFD fiscalization

## Status
Accepted — Task 69.

## Context
Task 071 requires sale, refund and correction fiscal workflows without committing the platform to a specific KKT/OFD vendor. Fiscal state and marking links must be durable and idempotent.

## Decision
1. Define provider-neutral receipt/refund/correction requests using exact Money and explicit idempotency.
2. Extend Connector SDK v1 additively with capability-specific fiscal receipt/status interfaces while keeping the root Connector/Runtime frozen.
3. Keep provider integration behind a host gateway interface.
4. Persist fiscal requests, marking fingerprints and append-only remote status evidence.
5. Reconciliation/status from the fiscal provider is authoritative.

## Alternatives considered
- Vendor-specific fiscal fields in Core: rejected.
- Mutable single status column without evidence: rejected because fiscal history must be auditable.
- Floating-point totals: rejected by the money contract.

## Compatibility impact
All changes are additive. Public commerce models and Connector SDK root interfaces are unchanged; only optional capability-specific fiscal interfaces are added.

## Migration and data impact
Migration `000044` adds fiscal requests, marking links and append-only fiscal status evidence with tenant RLS.

## Operational impact
A production KKT/OFD adapter must separately qualify credentials, legal receipt schema, receipt delivery and provider-specific error reconciliation. Failed/unknown fiscal states must alert rather than auto-complete.

## Security and privacy impact
No raw card data is accepted. Marking links use bounded fingerprints/references. Credentials and regulated provider payloads remain outside generic logs.

## Consequences
The platform gains a stable fiscal boundary that can support reference and commercial KKT/OFD adapters later without changing business workflows.
