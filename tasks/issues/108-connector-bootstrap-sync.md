# Task 108: Connector Bootstrap and Sync Schedule

## Status
`repository-complete`

## Objective
Add dry-run, initial import and durable synchronization scheduling for configured connector accounts.

## Dependencies
107, 009, 013, 014

## Acceptance
- dry-run is available before the first remote write;
- schedules create durable jobs and never rely on browser state;
- retries are limited to retry-safe operations with jitter;
- import/sync checkpoints are resumable and tenant-scoped;
- progress and reconciliation evidence are observable.

## Implementation evidence

- `POST /api/v1/connector-accounts:bootstrap-preview` persists 30-minute, account-version-bound metadata evidence after validating every enabled policy against current cabinet capabilities. It performs no connector transport and stores no payload or credential.
- `POST /api/v1/connector-accounts:bootstrap` consumes preview evidence with at least one inbound policy once and creates a tenant-scoped resumable `initial_import` dispatch job. `PUT /api/v1/connector-accounts:schedule` owns durable per-cabinet interval/mode/version state; `GET /api/v1/connector-accounts:bootstrap` exposes previews, schedules and job progress.
- Migrations `000062`–`000063` add forced-RLS evidence/schedule/job tables and the bounded cross-tenant lease function. The scheduler reapplies each returned tenant scope, revalidates active/healthy account state, manifest capabilities and current cabinet grants, and fans out deterministic reconciliation runs.
- Job fan-out resumes after `checkpoint_policy_id`; each reconciliation run retains the entity/page cursor from Tasks 013–014. Only database dispatch failures are retried, with a five-attempt bound and deterministic jitter; account/capability/run-identity failures are terminal.
- The Settings integration catalog and the main Synchronization page provide per-cabinet dry-run, initial-import, mode/interval controls and current dispatch progress next to manual sync operations. Browser state is presentation-only.

`completed` on a Task-108 job means that durable reconciliation runs were created for all eligible policies. Entity/page import completion remains observable on the corresponding reconciliation runs and Task-013 checkpoints; the scheduler does not misreport connector transport as completed.
