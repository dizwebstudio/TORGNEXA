# Task 113: Production Worker Composition Root

## Status

`repository-complete` — 2026-08-17.

## Objective

Replace the placeholder worker lifecycle with the production asynchronous
composition root for durable PostgreSQL work, transactional outbox publication,
Kafka consumption, webhook delivery, reconciliation execution and optional
upload-security scanning.

## Dependencies

007, 008, 009, 013, 014, 021, 063, 088b, 092b, 093, 108

## Acceptance

- `cmd/worker` starts a supervised runtime rather than the placeholder
  `background.Run` sleep loop;
- PostgreSQL, encrypted SecretProvider, outbox relay and concrete Kafka
  producer/consumer adapters are composed at startup and fail closed;
- outbox publication is tenant scoped and Kafka consumption preserves the
  existing at-least-once retry/DLQ contract;
- durable webhook delivery is driven from canonical Kafka events and scoped
  PostgreSQL delivery leases;
- reconciliation and upload work use bounded cross-tenant dispatch functions
  that expose only scope/item identities, then re-enter tenant RLS for domain IO;
- reconciliation never reports success when no provider-neutral connector
  source bridge exists; the job is released for bounded retry with a stable
  machine code;
- the upload worker uses immutable S3 quarantine reads, digest verification,
  ClamAV scanning and released-object promotion when explicitly enabled;
- all long-lived components share cancellation and are joined during graceful
  shutdown;
- worker defaults consume every transport topic represented in the canonical
  event catalog, with a drift test preventing silent topic omissions.

## Implementation evidence

- `internal/app/worker` owns composition, supervision and durable job loops.
- `internal/platform/kafkatransport` adapts `franz-go` to the neutral
  `kafkaeventbus` producer/reader interfaces.
- `internal/platform/postgres/workerrepo` owns bounded runtime leases backed by
  migration `000067_worker_runtime_dispatch.sql`.
- `internal/platform/uploads/s3.go` now implements bounded quarantine reads and
  digest-verified promotion needed by the security pipeline worker.
- `docker-compose.yml` and `.env.example` expose the worker-owned broker,
  polling, lease and optional ClamAV settings.

## Deliberate follow-up

Provider packages still do not expose production HTTP transport adapters that
can be converted to the canonical `reconciliation.Source` boundary. Task 113
therefore wires the resolver and executor but keeps the production source
registry fail-closed until that provider-neutral bridge is implemented; no
reconciliation run is falsely marked complete.
