# Auto.ru reconciliation

A successful feed submit returns a remote task ID. TORGNEXA stores that receipt through its normal host-side audit/mapping path and polls `classified.publications.status.read` until the provider reports `SUCCESS` or `FAILURE`. Provider counters are projected as total, inserted, updated, deleted, skipped, errors and notices.

A transport failure or remote 5xx during `classified.publications.write` is ambiguous: Auto.ru may already have accepted the feed. The connector therefore returns `write_outcome_unknown` with no retry delay and must not automatically submit the same feed again. Reconciliation first checks provider feed history and current dealer listings; only an operator/policy-authorized path may decide to resubmit.

Read pagination is bounded and opaque to Core. Listing state remains a remote observation rather than a replacement for TORGNEXA product/catalog source of truth.
