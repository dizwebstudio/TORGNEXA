# ADR-0117 — Returns, cancellations and refunds as separate aggregates

Status: Accepted

## Context

The existing order snapshot is immutable and the payment refund API is a
deliberately small lower-level primitive. Treating a customer cancellation,
physical return and money refund as one status would lose evidence and make
partial or repeated operations unsafe. A remote timeout can also leave the
provider result unknown; retrying such a command blindly can charge or refund
twice.

## Decision

Task 164 introduces provider-neutral `order_cancellations`, `commerce_returns`,
`return_items`, `return_inspections` and `refund_allocations` aggregates. They
keep tenant-scoped references to the canonical order, order item, payment,
refund and return, while the order and payment repositories remain the only
owners of their own state.

Cancellation and return transitions are validated in the core domain and
persisted with optimistic versions and append-only state history. A return
line stores exact decimal requested/received/accepted quantities and an
explicit disposition (`restock`, `quarantine`, `scrap`, `replace`). Refund
allocations are exact minor-unit amounts and are serialized on the payment so
the sum of pending, accepted, succeeded, unknown and manual-attention refunds
cannot exceed the captured amount.

The existing `payments.Refund` lifecycle remains the only payment mutation
path. It gains `unknown` and `manual_attention` outcomes for ambiguous remote
results; reconciliation or an operator must establish the final outcome before
another remote command is issued. New aggregate mutations emit canonical
events through the transactional outbox and record sanitized audit summaries;
raw provider payloads and credentials are excluded.

All irreversible outbound actions are still subject to Task-017 policy and
approval. Fulfilment, logistics, WMS, fiscal and settlement side effects are
follow-up capability ports and workers; this change does not silently mutate
inventory or fiscal documents when a return is merely requested.

## Consequences

Partial returns, multiple refunds, inspection evidence and idempotent retries
are represented without rewriting historical order or payment facts. Unknown
remote outcomes are visible to reconciliation instead of being misclassified
as failures. The additive migration requires a backup and preserves old
readers and writers, while the new API is fail-closed behind tenant permissions.

The current release does not claim live connector qualification for every
provider or automatic warehouse/fiscal execution; those remain explicit
runtime/conformance gates tracked in Task 164.

## Security and privacy

Every table is tenant scoped and forced through PostgreSQL RLS. Evidence uses
bounded IDs, hashes and machine reason codes. Payment credentials, tokens,
authorization headers and raw remote responses are never persisted or emitted.

## Compatibility impact

The existing payment refund endpoint and aggregate remain compatible. The new
return and cancellation endpoints and event schemas are additive; old readers
and writers can continue to operate while migration 29 is expanded.

## Migration and data impact

Migration `000029_returns_cancellations_refunds.sql` adds tenant-scoped tables,
indexes, policies and append-only evidence triggers. It does not rewrite order,
payment or inventory history and is backup-gated with a checksum in the
migration catalog.

## Security and privacy impact

The API requires tenant permissions and idempotency keys. Stored evidence is
bounded to references, reason codes and digests; credentials, card data and
raw remote payloads are excluded.

## Operational impact

Unknown remote outcomes are reconciled or sent to manual attention rather than
blindly retried. Operators can disable the new routes/policies independently;
live connector, WMS, fiscal and settlement qualification remains a release
gate.

## Alternatives considered

- Overload `Order.status` or `Payment.status`: rejected because a physical
  return and a money refund have independent lifecycles and evidence.
- Treat every timeout as `failed`: rejected because the remote side may have
  accepted the operation, causing a duplicate on retry.
- Store raw provider responses: rejected for secret/PII minimization and
  provider portability; only bounded evidence and hashes are retained.
