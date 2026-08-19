# Task 007 — EventBus + Kafka Adapter

Status: repository-completed (2026-08-09)

Implement EventBus abstraction + Kafka adapter; domain packages must not import Kafka.

## Acceptance

- Broker-neutral canonical Event/EventType/Delivery types validate tenant, aggregate, UTC, correlation and bounded JSON-object payload metadata.
- Domain/Core code imports no Kafka client package.
- Kafka adapter deterministically maps event family/version to topic and tenant aggregate to partition key.
- Publisher refuses unsafe producer capabilities unless idempotence and `acks=all` are enabled.
- Consumer implements bounded at-least-once retry/DLQ routing; source offsets are committed only after successful handler processing or successful retry/DLQ publication.
- Retry metadata is bounded, UTC, machine-readable and contains no raw handler error text.
- Invalid envelope/header/topic/retry metadata never reaches the business handler.
- Tests cover duplicate JSON keys, topic/key mapping, producer requirements, success, retry/backoff, permanent failure, exhausted retries, poison records, tampered headers, cancellation, and publish-before-commit behavior.
- Event transport contract/docs and architecture review are updated; repository checks pass.

## Follow-up boundary

Task 008 supplies the transactional PostgreSQL outbox and relay. Task 009 supplies consumer inbox/deduplication. EventBus intentionally remains at-least-once and does not pretend that Kafka publish + offset commit is a cross-system exactly-once transaction.
