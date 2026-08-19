# Task 062: Developer SDK generation

## Status
Repository-complete. External registry publication remains governed by Task 065 release/license qualification; repository acceptance is deterministic generation plus local compile/test evidence.

## Objective
Generate supported Go/TypeScript/Python clients from the public OpenAPI and publish examples/version policy without exposing internal database models.

## Dependencies
006

## Deliverables
OpenAPI-only deterministic generator with drift checking; committed Go, TypeScript and Python client artifacts; source/hash/operation manifest contract; language tests and examples; SDK compatibility/version policy; CI/repository check integration; architecture/governance evidence.

## Acceptance
All OpenAPI `operationId` entries are represented in the generated manifest and clients. `make sdk-check` proves deterministic regeneration, Go compile/tests, TypeScript runtime/tests plus declaration compilation when `tsc` is available, Python compile/tests, source SHA-256 parity and absence of `internal/*`, PostgreSQL/sqlc/ORM/driver model tokens from public SDK trees. SDK request scope remains API-authenticated: generation must not synthesize organization/workspace selectors that are absent from public OpenAPI.
