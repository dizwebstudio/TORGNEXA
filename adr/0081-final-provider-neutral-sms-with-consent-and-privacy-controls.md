# ADR 0081: Provider-neutral SMS with consent and privacy controls

## Status
Accepted

## Context
Task 091 adds SMS delivery to Notification Center but phone numbers are PII and marketing messages require an independent consent policy.

## Decision
Define transactional/marketing message classes, provider/status/callback ports, tenant quotas, consent checks, phone fingerprint evidence and a deterministic fake provider.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Persisting raw phone numbers in delivery evidence and allowing marketing through the transactional path were rejected.

## Compatibility impact
Existing web UI/webhook notification channels are unchanged; SMS is additive.

## Migration and data impact
Expand-only migration 000054 stores phone fingerprints, delivery evidence and deduplicated callbacks under forced RLS.

## Security and privacy impact
Marketing requires a positive consent decision; phone numbers exist only in the ephemeral provider request while durable evidence stores SHA-256 fingerprints.

## Operational impact
Notification fallback can treat SMS failures as bounded provider failures without bypassing consent or quotas.
