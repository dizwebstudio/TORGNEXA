# ADR-0194 — Bounded OAuth refresh admission and telemetry

Status: Accepted

## Context

ADR-0104 moved OAuth refresh, locked re-read and encrypted rotation onto one
PostgreSQL transaction/connection. This removed the nested-transaction
deadlock, including with `MaxOpenConns=1`, but refreshes for different accounts
could still occupy every connection in a larger process pool. Advisory-lock
contention used a fixed 25 ms retry interval, which synchronized replicas under
load, and the runtime exposed no direct measurements of admission pressure,
lock wait, refresh failures or pool saturation.

An OAuth provider may consume or rotate a refresh token before the client sees
the response. Retrying the remote request after a timeout or connection error
is therefore unsafe unless provider state is reconciled first. Backoff must be
limited to the local, side-effect-free advisory try-lock operation.

## Decision

Each API or worker `secretrepo.Repository` owns one process-local OAuth refresh
admission semaphore shared by every account-aware connector runtime constructed
from it. Its capacity is eight when PostgreSQL has no explicit maximum. With a
bounded pool, capacity is one for a one-connection deployment and otherwise
`min(8, MaxOpenConns-1)`. This preserves one connection for non-refresh work
when the pool has more than one slot. API and worker replicas retain the
tenant/reference PostgreSQL advisory lock for cross-process serialization.

An occupied advisory lock is retried with equal jitter over one half to the
full exponential ceiling. Ceilings are 25, 50, 100, 200 and then 250 ms. The
request context bounds admission and lock wait. Provider HTTP refresh is
attempted at most once after lock acquisition; token endpoint errors are
returned for reauthorization or reconciliation and are never passed through
this retry loop.

The repository exposes a label-free, process-local metrics snapshot with:

- concurrency limit, current/peak in-flight and current waiters;
- admission wait/cancellation counters and a fixed-bucket wait histogram;
- advisory-lock attempts/contentions and a fixed-bucket wait histogram;
- successful/failed refresh counts and an end-to-end refresh/rotation latency
  histogram, where a transaction commit failure counts as failed;
- current `database/sql` open/in-use/idle/wait values and current/peak pool
  saturation in parts per million.

The snapshot accepts no tenant, account, connector, URL or secret labels. An
OpenTelemetry/Prometheus adapter may map the fixed fields to its local naming
convention without increasing attacker-controlled cardinality.

## Compatibility and migration impact

Public REST/OpenAPI, events, Connector SDK and stored secret formats are
unchanged. No database migration or backfill is required. API and worker may be
rolled out in either order because PostgreSQL remains the shared serialization
boundary.

The minimum or maximum PostgreSQL pool size is not raised. The refresh limit is
derived once from the already validated process pool during repository startup;
it is an application guard rather than a pool-size mitigation.

## Security and privacy impact

Bounded admission limits connection exhaustion caused by simultaneous expired
accounts. Jitter reduces synchronized try-lock traffic between replicas.
Metrics contain only process counters, durations and pool sizes. Credential
material, opaque secret references, tenant/account identifiers, token endpoint
responses and errors remain absent from telemetry.

## Operational impact

Alert on sustained admission waiters, increasing lock contention, refresh
failure rate and pool saturation. A one-slot pool remains supported for small
deployments but can reach 100% saturation during a refresh; larger pools retain
one slot outside the refresh admission budget. Changing `MaxOpenConns` after
repository construction is unsupported process reconfiguration and requires a
restart, as does the existing database pool configuration.

## Validation

Unit tests prove exponential ceilings, jitter bounds, cumulative latency
buckets and failure accounting after a coordinator commit error. The disposable
PostgreSQL gate runs under forced RLS and `go test -race`; it covers one- and
four-connection pools, ten different accounts
against a twelve-connection pool, queued admission, pool saturation, contended
advisory-lock retry, cancellation, provider rejection, commit/rotation rollback
and rotating-token replay.

Evidence is recorded in
`docs/audits/2026-09-12-oauth-refresh-admission.md`.
