# Task 063: Durable webhook service

## Status
Repository-complete. Live PostgreSQL/RLS execution, production DNS/egress policy and worker SLO qualification remain deployment evidence, not missing repository implementation.

## Objective
Implement signed tenant-scoped webhooks, retries/DLQ/history/replay/rotation and SSRF controls.

## Dependencies
007, 021

## Deliverables
Provider-neutral durable webhook service, PostgreSQL repository and migration, HMAC signature/verification contract, HTTPS/DNS SSRF boundary, lease-based retry worker, immutable attempt history and DLQ/replay flows, signing-secret rotation, authenticated management API, Draft 2020-12/OpenAPI contracts, tests, architecture evidence and operational documentation.

## Acceptance
Contract tests cover current/previous signatures, tampering, stale replay windows, retry/backoff, replay identity, permanent DLQ/disable behavior and DNS-rebinding fail-closed behavior. Tenant scope comes only from authenticated/event context; PostgreSQL repeats explicit tenant predicates under forced RLS. Request snapshots and attempt history are mutation-guarded, response bodies/raw remote errors are not persisted, signing material remains behind Task-021 SecretProvider references, and required repository checks pass.
