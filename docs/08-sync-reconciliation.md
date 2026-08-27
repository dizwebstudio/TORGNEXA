# Bidirectional Sync & Reconciliation

## Task 013 — Sync Engine

The sync engine is provider-neutral. Core entities never gain WB/Ozon/ERP identifiers; the existing Connector SDK `EntityMapping` is the only local ↔ remote identity bridge.

Production admission is exact and fail-closed. A manifest capability does not
make an entity executable: the connector, entity and direction must also be
declared in `contracts/connectors/builtin-runtime-support-v1.json` and resolved
by the built-in runtime. The current worker bridge accepts only the canonical
`products` entity. Unsupported policies are rejected by the API before they can
be enabled or dispatched, and the worker retains its own entity check as a
second boundary.

A versioned `SyncPolicy` binds one tenant-scoped connector account and canonical entity type to:

- direction: `inbound`, `outbound`, or `bidirectional`;
- source-of-truth conflict rule: `local`, `remote`, or `manual`;
- enabled/disabled lifecycle with optimistic versioning.

Source-of-truth is **not** an unconditional write bypass. Ordinary propagation follows direction. It is consulted only when both the canonical local version and remote revision advanced from the last successfully synchronized state.

### Durable propagation

Inbound pull uses a persistent cursor. The cursor advances only after every change in a page resolves. A crash before cursor commit replays the page; append-only remote receipts and stable local idempotency keys prevent duplicate business effects.

Outbound local changes use the immutable domain event id to derive a stable remote idempotency key. A crash after the provider accepted the write but before local state/receipt commit retries the same key. Connector implementations are therefore required to provide the same idempotency semantics already exercised by Task `064` conformance.

Task 013 does not claim distributed exactly-once transactions.

### Correlation, causation and loop prevention

Outbound requests preserve the local correlation id (or event id when absent), set causation/origin to the local event id and carry a policy-scoped origin source `sync.<policy-hash>`.

Inbound local mutations use a deterministic event/idempotency id, keep valid remote correlation metadata where possible, use the remote change id as causation, and write the same policy-scoped origin source.

Loop prevention has two layers:

1. an inbound local event is never reflected back through the **same** sync policy because its policy-scoped source is recognized;
2. providers that cannot round-trip origin metadata are protected by a canonical JSON payload SHA-256 fingerprint stored with the last synchronized entity state.

The marker is policy-scoped rather than globally `source=sync`, so a change received from one connector may still propagate to a different connector when its policy allows it.

### Conflict behavior

When both sides changed since the last sync:

- `local` source-of-truth rejects the inbound remote overwrite and allows an outbound optimistic write to retry explicitly as a local-authoritative overwrite after a bounded remote revision conflict;
- `remote` source-of-truth allows inbound overwrite of the observed local version and refuses an outbound overwrite after a remote revision conflict;
- `manual` fails closed with `ErrConflict` and does not advance the inbound checkpoint.

Task `014` owns durable drift records, on-demand/full reconciliation and safe remediation actions. Task 013 intentionally does not create a second reconciliation subsystem.

## Task 014 — Reconciliation boundary

Reconciliation modes: incremental, scheduled full, on-demand. Detect drift, missing/orphan/duplicate mappings, status mismatch and stale connectors. Actions: safe auto-fix, notify, approval or explicit ignore.

Connector health ultimately includes auth, sync lag, drift/error rate and reconciliation success, not only ping.

### Task 014 implementation

Task 014 implements one provider-neutral reconciliation runner for `incremental`, `scheduled_full`, and `on_demand` scans. A host scan adapter supplies bounded canonical/remote snapshots; the reconciliation engine does not perform transport-specific network IO and does not duplicate Task-013 propagation.

Detected drift classes are:

- `content_drift` — uniquely mapped local/remote snapshots disagree by canonical SHA-256 fingerprint;
- `missing_mapping` — unique local and remote subjects exist without an identity mapping;
- `orphan_mapping` — a mapping exists while one side no longer exists;
- `duplicate_mapping` — scan evidence reports non-unique local or remote mapping cardinality;
- `status_mismatch` — uniquely mapped status projections disagree;
- `stale_connector` — the remote observation watermark exceeds the configured freshness window.

Runs persist cursor/progress and may resume from `running` or `interrupted` state. Drift IDs are deterministic within a run, so replaying the same page cannot duplicate drift evidence or inflate counters. Completed runs are immutable.

Remediation is deliberately narrower than detection. `auto_fix` is permitted only for an unambiguous mapping creation explicitly marked safe by the scanner, or for content/status repair where Task-013 source-of-truth and direction select exactly one authoritative write direction. Manual source-of-truth, orphan/duplicate mapping and stale health never auto-fix. `notify`, `approval`, and explicit `ignore` are separate actions. External actions receive a deterministic idempotency key; failed attempts persist only a bounded machine error code, never raw remote/provider text.

A crash after drift insertion or after an external remediation effect is safe to replay: an open drift is retried with the same action idempotency key until action evidence/status is durably committed.

## Task 108 — Bootstrap and durable schedules

The first-account workflow starts with an immutable, metadata-only dry-run. It
counts enabled read/write policies after current manifest and per-account
capability authorization; it never opens credentials or calls a provider. The
evidence is tenant scoped, account-version bound, expires after 30 minutes and
is required before initial import, schedule enablement and the first manual
remote write.

Initial imports and due schedules create PostgreSQL jobs. The scheduler claims
them through a bounded cross-tenant lease function, then immediately reapplies
the returned organization/workspace scope for all account, policy and run
operations. It rechecks account health and capabilities at execution time.
Policy fan-out uses stable ordering, a durable `checkpoint_policy_id` and
deterministic reconciliation run IDs, so a lease loss resumes without duplicate
runs. Reconciliation itself retains the entity/page cursor described above.

Retries cover only retry-safe database dispatch operations, are bounded to five
attempts and include deterministic jitter. Invalid account/capability state and
run-identity collisions fail terminally with bounded machine codes. A completed
bootstrap job means fan-out is complete; import/drift completion is read from
the linked reconciliation runs rather than inferred from the dispatch row.
