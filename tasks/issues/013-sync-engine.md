# Task 013

Mappings, source-of-truth/direction, correlation/causation and loop prevention; conflict tests.

## Acceptance
Implementation + tests + updated contracts/docs; run required checks.

## Status
Repository-complete.

## Implementation evidence
- Provider-neutral `internal/platform/syncengine` owns versioned direction/source-of-truth policies, inbound page checkpoints, conflict decisions, canonical JSON fingerprints, deterministic idempotency keys and policy-scoped loop markers.
- Existing Connector SDK `EntityMapping` remains the only local/remote identity bridge; provider IDs are not added to Core.
- `internal/platform/postgres/syncrepo` + migration `000024_sync_engine.sql` persist forced-RLS policies/checkpoints/entity states and immutable inbound/outbound receipts.
- Crash/retry tests prove remote write replay and partial inbound-page replay without duplicate business effects; manual conflicts do not advance the checkpoint.
- Source-of-truth tests cover local/remote/manual conflict behavior; loop tests cover explicit origin metadata, providers without origin round-trip, and cross-policy propagation.
- Task `014` remains responsible for drift records and reconciliation actions.
