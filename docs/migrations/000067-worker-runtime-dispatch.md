# Migration 000067: Worker runtime dispatch

Task 113 introduces a dedicated lease queue for cross-tenant asynchronous worker
execution without weakening tenant RLS on domain tables.

`worker_runtime_jobs` stores only `kind`, organization/workspace identity,
canonical item identity, availability, lease fencing metadata, bounded attempt
count and a machine error code. It does not duplicate event payloads, upload
bytes, connector credentials or remote response bodies.

The table enables and forces RLS. Normal application access remains tenant
scoped. Four narrowly scoped `SECURITY DEFINER` functions run with row security
disabled only to coordinate the shared worker fleet:

- `list_worker_active_scopes` returns tenant scopes with due outbox/webhook work;
- `claim_worker_runtime_jobs` materializes and leases eligible reconciliation or
  upload identities with `FOR UPDATE SKIP LOCKED`;
- `release_worker_runtime_job` clears a matching fenced lease and schedules a
  bounded retry;
- `complete_worker_runtime_job` deletes only the matching leased runtime queue
  entry after domain work reaches a terminal state.

The functions expose identity/lease metadata only. The worker must re-enter the
returned organization/workspace scope before reading or mutating reconciliation,
upload, outbox or webhook domain rows.

The migration is additive (`expand`, high risk) and does not change existing
domain schemas. Rollback before application adoption may remove the new queue and
functions; after adoption, prefer a forward repair so active lease coordination
is not silently removed from a running worker fleet.

## Rolling compatibility

The new worker treats undefined Task-113 dispatch functions/tables as a temporary schema-unavailable state and keeps the process alive without claiming cross-tenant work. This preserves new-binary/old-schema rollout compatibility; background processing becomes active after `000067` is applied.
