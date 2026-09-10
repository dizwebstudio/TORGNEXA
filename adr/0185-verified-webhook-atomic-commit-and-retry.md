# ADR-0185 — Verified webhook commit and retry

Status: Accepted

## Context

Audit A03/A04 and Task 234.2 found that payment receipts committed before status
changes. A failed change consumed the delivery ID permanently. All inbound
payment/commerce/social handlers also acknowledged persistence failures with 200.

## Decision

Payments use one `ApplyVerifiedWebhook` repository operation for the normalized,
independently verified observation. The receipt, payment change, authoritative
audit and Transactional Outbox share the tenant/pool-bound transaction introduced
by ADR-0184. Only its successful commit allows the provider acknowledgement.
Concurrent deliveries serialize on the receipt key and payment row; an optimistic
conflict rolls back the receipt and requests redelivery. Matching current status
records the receipt without another status event. Matching committed deliveries
are no-ops; a reused delivery ID with different evidence fails closed.

After successful independent verification, a persistence/commit error produces
HTTP 503, an empty JSON object, `Retry-After: 5` and `Cache-Control: no-store`.
Provider-specific success tokens are returned only after commit. Rejections
before verification retain the uniform 200 response from ADR-0105. This decision
supersedes its unconditional-200 wording for verified persistence failures.

Commerce/social retain their existing atomic Inbox/Outbox. Host-owned wrappers
observe failures at `ClaimCommerceWebhook` / `ClaimSocialWebhook`, which qualified
connectors invoke only after authenticity and payload checks. This avoids
classifying wrapped connector or secret-store errors as proof of verification.
The host tracks commit failure even if a connector wraps or suppresses its error.
No provider names or provider-specific retry logic enter Core or API dispatch.

Commerce receivers may infer occurrence time from arrival when the provider body
has no timestamp. For their content-addressed delivery IDs, Inbox retains the
first committed observation time on replay, compares all other envelope fields,
and still rejects payload collisions. Timestamps are canonicalized to PostgreSQL
microseconds before fingerprinting. Ordinary EventBus fingerprinting is unchanged.
Old receipts whose original nanosecond timestamp was lost to database precision
can still conflict and require reconciliation; immutable history is not rewritten.

The existing top-level `webhook_secret_reference` configuration key is admitted
only with a valid opaque SDK SecretReference. The blanket sensitive-key filter
previously rejected it, preventing social webhook configuration. Plaintext,
nested aliases, null/non-string values and all other sensitive keys stay denied.

## Compatibility and migration

No SQL migration or new persisted state. Events and business payloads are
unchanged. OpenAPI documents 503 for the three ingress families and the existing
payment callback route; generated clients are refreshed. Deploy API and worker
from the same revision. Callback operators must allow provider retry on 503.
The subscription references required by ADR-0184 remain mandatory for commerce.

Old payment receipts cannot prove that their status change committed before this
fix. Upgrade does not rewrite append-only history. Reconcile payments spanning
the affected interval, including receipts produced by older API instances, before
retiring the old deployment. New deliveries no longer create this gap.

## Security, privacy and operational limits

No new fields, personal data, raw bodies or credentials are persisted. Only
normalized verified evidence reaches PostgreSQL; retry responses and failure logs
omit database details. Tenant scope and exact pool binding remain enforced.

Account/config/secret lookup and provider verification outages still receive the
uniform pre-verification response; they do not establish authenticity and cannot
safely select the new verified-only response. Reconciliation remains the safety
net for these failures and for provider retry exhaustion. This change does not
introduce an unauthenticated durable queue or a receipt worker.

## Validation

Real disposable PostgreSQL with forced RLS and a one-connection application pool
covers receipt, transition, audit, outbox, deferred-commit and database-connection
failures, successful recovery, concurrent deliveries and replay. Actual admitted
commerce/social verifiers run against synthetic encrypted secrets and database
failure injection. No real remote write or payment is performed.
