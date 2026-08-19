# Task 117: P3 Warehouse Execution and Release Closure

## Status
`done` — repository implementation complete on 2026-08-18.

## Objective
Close the remaining P3 repository gap after Task 116: turn warehouse incident routing evidence into transactional fulfillment-reservation execution and make the exact execution path mandatory in production qualification and release CI.

## Scope
- add durable tenant-scoped `fulfillment_allocations` that bind one immutable order item to a warehouse reservation;
- reserve an order item only when the warehouse is administratively/operationally allocatable and ATP covers the exact quantity;
- on `UNAVAILABLE`/`LOST`, release tracked source allocations and create destination replacement allocations in the same PostgreSQL transaction that moves reserved quantity between positions;
- preserve physical on-hand truth: failover changes reservation ownership, never physical stock;
- emit inventory position events and `commerce.fulfillment.allocation_changed.v1` outbox evidence for every executed reroute;
- fail closed on insufficient capacity, terminal orders, allocation inconsistencies, or untracked legacy reservations;
- expose fulfillment allocation read/write endpoints and regenerate public SDKs;
- require the deployed-image runtime qualifier in the protected release workflow;
- resolve the repository license metadata to Apache-2.0 while retaining independent vulnerability, provenance, hosted-policy and topology gates;
- provide `make p3-qualification` as the combined repository/topology qualification command.

## Safety invariants
- physical `on_hand` never increases or moves as a side effect of warehouse failover;
- warehouse identity on an allocation is immutable; reroute creates a replacement allocation with lineage to the released source allocation;
- source reserved decreases by exactly the tracked rerouted quantity and destination reserved increases by exactly the same quantity, atomically;
- destination ATP is locked/rechecked inside the transaction before any reservation mutation commits;
- terminal orders are never given replacement allocations;
- reservations that cannot be mapped to immutable order items are never guessed; the incident records `needs_attention`;
- every execution is tenant scoped under FORCE RLS and produces audit/outbox lineage;
- deployment qualification fails closed if Docker, PostgreSQL, Kafka, worker recovery, or fulfillment reroute evidence is unavailable.

## Deliverables
- migration `000074_fulfillment_failover_execution.sql`;
- fulfillment allocation domain/repository/API and generated public SDK operations;
- event `commerce.fulfillment.allocation_changed.v1` and fixture/catalog registration;
- transactional warehouse incident execution and execution counters;
- upgraded runtime qualifier proving source reservation release, destination reservation creation and fulfillment outbox evidence;
- mandatory `runtime-qualification` job in `.github/workflows/release.yml`;
- `scripts/check-p3-release-qualification.sh` / `make p3-qualification`;
- approved Apache-2.0 repository license metadata;
- ADR-0092 and ARCH-117 review.

## Acceptance
- inventory domain/repository tests compile and pass;
- migration catalog validates with 74 migrations and latest `000074`;
- event schema valid/invalid fixtures validate and event catalog remains sorted/unique;
- generated public SDKs are current at 107 OpenAPI operations;
- architecture, frontend, JS supply-chain and Community deployment gates remain green;
- the release workflow cannot reach build/publish unless runtime qualification passes;
- a deployment may claim P3 runtime qualification only from retained evidence generated on the exact Docker-capable release topology;
- Task 065 protected OIDC and Task 080 hosted Ruleset evidence remain external facts and are never fabricated by repository completion.
