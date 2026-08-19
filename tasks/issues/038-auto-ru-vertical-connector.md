# Task 038: Auto.ru vertical connector

## Objective
Audit partner/feed/API availability and implement Vehicle publication/status integration through Vertical/Classified SDK.

## Dependencies
010, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
No Core branching; vehicle mapping spec and conformance tests.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

Implemented and qualified:
- architecture-registered `auto-ru` classified provider behind Connector SDK v1;
- exact dealer-account health binding against `/1.0/dealer/account`;
- bounded passenger-car offer reads with current `car_info`/pagination response mapping;
- additive provider-neutral classified publication/status SDK capabilities;
- NEW/USED passenger-car XML feed mapper and validation profile;
- manual feed submission plus asynchronous task-status reconciliation;
- fail-closed non-retryable `write_outcome_unknown` on ambiguous feed writes;
- deterministic provider tests, vehicle mapping spec, capability audit, reconciliation guide and Task-064 conformance evidence.

Task-064 provider conformance: **13/13 PASS**, report SHA-256 `deb6b2b7c642b81c87b14f703855688723838fd228f862c7ff54191ab255f73f`.

No Core provider branch, database migration, public OpenAPI operation or EventBus schema was added. Next classified task: `039 CIAN Connector`.
