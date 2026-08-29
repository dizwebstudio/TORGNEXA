# Task 116: P2 Production Qualification Closure

## Status
`done` — repository implementation complete on 2026-08-18.

## Objective
Close the second production-readiness layer after Tasks 114-115: durable warehouse incident automation, deployment-level runtime/load/failure qualification, and a narrowly qualified marketplace write surface.

## Scope
- convert `UNAVAILABLE`/`LOST` warehouse transitions into durable tenant-scoped worker jobs;
- process affected offers incrementally and retain append-only failover evidence without fabricating stock movement;
- expose operational warehouse state, failover routes and incident history through the public API;
- qualify Outbox -> Kafka -> Inbox, duplicate-event idempotency, and durable warehouse incident routing from the deployed application image;
- execute worker, Kafka and PostgreSQL restart drills plus a bounded API black-box load profile;
- add Yandex Market `prices.write` only against documented exact-price endpoints;
- repair architecture-policy regressions discovered while running the P2 gate.

## Safety invariants
- A warehouse incident never changes `inventory_positions.warehouse_id`, invents a transfer, or increases stock at a destination.
- A failover decision is `routed` only when an explicit enabled route points to an active/degraded warehouse with positive ATP for the same offer.
- `needs_attention` is an acceptable fail-closed terminal incident state when no eligible destination exists.
- Kafka duplicate delivery must leave exactly one immutable Inbox receipt and consumer progress must continue to a later marker event.
- The deployed-image probe must also prove one `LOST` warehouse incident routes to the configured positive-ATP backup while source physical stock remains unchanged.
- Runtime qualification fails closed if Docker, a required service, a load threshold, or a post-failure probe is unavailable.
- Marketplace write capability is declared only where the current provider-neutral SDK can faithfully represent the remote mutation.

## Deliverables
- migration `000073_warehouse_incident_automation.sql`;
- persistent incident repository and supervised worker loop;
- warehouse operations/failover incident API and regenerated public SDKs;
- `/app/torgnexa-runtime-qualifier` in the application image;
- `scripts/runtime-load.py` and `scripts/check-production-qualification.sh`;
- qualification-only Compose overlay `docker-compose.qualification.yml`;
- Kafka reader/consumer recovery with bounded backoff for broker fetch and
  commit/retry failures;
- `make production-qualification`;
- Yandex Market `prices.write` adapter and conformance tests;
- ADR-0091 and ARCH-116 review.

## Acceptance
- `make architecture`, migrations, contracts, generated SDK, frontend and supply-chain gates remain green;
- Yandex Market connector tests prove both campaign and business-wide price update paths and fail closed on invalid remote responses;
- repository qualification command is reproducible and records deployment evidence under `qualification/evidence/<UTC timestamp>`;
- the disposable qualification Compose project uses the explicit
  `TORGNEXA_QUALIFICATION_RATE_PER_MINUTE` budget (default `10000`) for the
  burst probe, so API availability is measured independently of the normal
  production per-IP throttle; the deployment default is not changed;
- Kafka/PostgreSQL restart drills wait for a fresh healthy container state and
  a stable worker runtime before sending the next probe event, so a transient
  broker reconnect cannot create a false timeout;
- production release may claim runtime qualification only after that Docker gate passes on the exact release topology; repository completion alone does not manufacture deployment evidence.
