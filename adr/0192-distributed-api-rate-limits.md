# ADR 0192: API rate limits are shared across replicas

## Status

Accepted for Task 234.8.

## Context

The API security composition used a process-local map and mutex. Every API
replica therefore granted a fresh request budget, so adding replicas multiplied
the effective limit. The same pre-authenticated IP counter also represented
authenticated tenant traffic, which made shared NAT traffic compete with one
principal's application budget. Attacker-selected IPs or identities could grow
the local map until periodic full-map cleanup ran in the request path.

## Decision

The security edge depends on a `Limiter` interface. Production API processes
must use the Valkey implementation and a deployment-shared namespace. One Lua
evaluation uses Valkey server time and atomically creates/increments a fixed
window counter, records its expiry, and enforces a bounded number of active
keys. All Lua keys use the same Valkey Cluster hash tag.

The API applies three independent budget classes:

1. pre-authenticated requests use the validated client IP;
2. authenticated requests use organization, workspace, issuer and subject;
3. public inbound webhooks use the validated source IP and a fixed webhook
   limit.

The adapter hashes the complete budget key with SHA-256 before storage. Raw IP,
tenant and principal values are not Valkey key names or sorted-set members.
Counter TTL is one window; cardinality metadata is expired and pruned with the
same bounded lifecycle and disappears within two windows after the last new
key.

Valkey is an admission dependency. API startup fails if it cannot connect, and
an ambiguous or failed request-time limiter operation fails closed with HTTP
503 and `Retry-After: 1`. A confirmed exhausted budget returns HTTP 429 with
`Retry-After` rounded up from the remaining Valkey TTL. Limiter operations are
never retried after a write because the outcome may already have committed.

The local implementation remains available only for development, tests and an
explicit single-process topology. It uses 64 lock shards, expiry heaps and an
atomic global key cap. The normal request path touches one shard; a bounded
64-shard expiry pass occurs only after cardinality capacity is reached.

## Alternatives considered

Dividing the configured limit by the declared replica count was rejected
because rolling updates and autoscaling make the count transient and requests
are not distributed evenly. Sticky load balancing was rejected because it
does not stop reconnects or deliberate source distribution from obtaining a
new budget. PostgreSQL counters were rejected because their write load and lock
contention do not belong in the transactional system of record.

## Compatibility impact

No request or response schema changes. Clients may now receive HTTP 429 based
on the deployment-wide budget and HTTP 503 while the admission dependency is
unavailable. Both responses include integer `Retry-After` seconds.

## Migration and data impact

No database migration is required. Rate-limit state is ephemeral Valkey data.
Changing the namespace starts a new set of counters and must be treated as an
operational policy change.

## Operational impact

Production API configuration requires `valkey`, a reachable `VALKEY_ADDR`, a
shared namespace, bounded pool timeouts and a maximum active-key count. An ACL
username may be supplied with its password. Operators should alert on any
increase in `Unavailable`, sustained `CapacityLimited`, or the structured
`security.rate_limiter_unavailable` event. A planned Valkey outage denies new
API requests until the dependency recovers.

## Security and privacy impact

Replica scaling no longer increases an attacker's allowance. Separate budgets
preserve shared-NAT usability while limiting one authenticated principal across
source IPs. Stored keys contain one-way digests of pseudonymous request data,
are bounded in count and expire automatically. Passwords are accepted only as
process secrets and never appear in limiter keys, responses or structured
events.

## Consequences

Valkey availability is part of production API admission availability. The API
keeps business handlers fail-closed and avoids treating Valkey as a source of
business truth.
