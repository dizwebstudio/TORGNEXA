# ADR 0119: Product Publication Quality Center

## Status

Accepted for the Task 166 foundation. Runtime wiring, API/UI and connector
qualification remain subject to the explicit gates documented below.

## Context

Publication currently assembles a small product payload in the sync worker,
while catalog/PIM, price, inventory, media release, mapping, capability and
compliance facts are owned by separate domains. A filled-in Product is
therefore not evidence that a particular connector account can accept an
Offer. Operators also need a target-specific explanation of every blocker,
warning and stale input before a remote write is attempted.

## Decision

1. Add a provider-neutral publication-quality engine. It evaluates a bounded,
   immutable snapshot against a versioned declarative profile and produces a
   deterministic score, category scores, issues and a terminal decision.
2. A successful run may issue a `PublicationGateReceipt`. The receipt binds
   target/account, product/offer and all source versions, profile and snapshot
   digests, capability version and the Task-082 compliance fingerprint. The
   host checks the receipt immediately before remote egress; any mismatch,
   expiry, unknown evidence or unsupported capability fails closed.
3. Profiles contain only a small typed rule vocabulary and bounded metadata.
   They cannot execute code, SQL, regular expressions, network calls or access
   credentials. Connector-specific names remain at the profile/adapter edge;
   the core never branches on provider IDs.
4. Quality records are derived facts. Product/PIM, price, inventory, media
   security release, mapping, capability and compliance domains remain the
   authoritative sources and keep their own write policies. A warning can make
   a receipt `ready_with_warnings`, but a blocker, approval requirement, stale
   input or unknown result never becomes a pass through score manipulation.
5. PostgreSQL stores tenant-scoped profiles, runs, issues and receipts with
   forced RLS and append-only terminal evidence. Events, worker scheduling,
   REST/UI and connector remote preflight are composed through existing
   EventBus, policy, outbox/inbox and connector boundaries in later slices.

## Alternatives considered

- Put all checks in each connector: rejected because policy, compliance,
  freshness and score semantics would diverge and provider branches would leak
  into core workflows.
- Reuse Product validation as publication readiness: rejected because a
  Product has no target account, capability snapshot, channel mapping or
  remote contract context.
- Let the worker re-evaluate ad hoc immediately before every write: rejected
  because retries would be non-reproducible and there would be no durable
  explanation or exact-match evidence for an audit.
- Treat an unavailable check as a warning: rejected; unknown and stale facts
  must fail closed for externally visible publication.

## Compatibility impact

The model is additive and does not change Product, Offer, connector SDK or
canonical event payloads. Existing writers remain compatible until they opt
into the quality receipt gate. New REST/events must use the existing `/api/v1`
and canonical envelope compatibility policies.

## Migration and data impact

Migration `000033_product_publication_quality.sql` adds only tenant-scoped
quality profiles, immutable runs/issues, gate receipts and remediation intent
metadata. It does not rewrite catalog, PIM, inventory, media, compliance or
publication history. Retention and purge jobs must preserve receipts needed by
audit while removing expired derived detail according to workspace policy.

## Security and privacy impact

All records are scoped by organization/workspace and protected by forced RLS.
Snapshots and issues contain bounded identifiers, versions, digests and safe
operator text; they never contain credentials, Authorization headers, raw
provider payloads, binary media, arbitrary HTML/JS or unnecessary customer PII.
Media is considered only after the upload-security/ClamAV release decision.
The receipt is a gate, not an approval bypass; policy/approval and compliance
guards remain authoritative.

## Operational impact

The local engine is deterministic and has no network side effects, so it is
safe to run in API previews and workers. PostgreSQL persistence and indexes are
expand-only. Metrics should expose decision distribution, stale/unknown rate,
issue categories, evaluation latency and receipt-denial reasons. Rollback is
performed by disabling the caller gate; no source-of-truth data is rewritten.

## Consequences

Operators get target-specific, reproducible publication readiness with
actionable remediation hints. A receipt makes a successful preflight explicit
and prevents a changed product or capability from being sent under old
evidence. The system carries additional derived storage and requires every
future publication route to preserve the same fail-closed gate semantics.
