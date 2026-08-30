# Kafka Event Platform

TORGNEXA uses Apache Kafka/KRaft as the durable replayable event transport. A
development environment may use one combined broker/controller; production
uses an HA controller quorum, replicated partitions, authenticated encrypted
listeners, ACLs, bounded retention, and monitored consumer lag.

Business/domain packages depend only on `internal/platform/eventbus`. Kafka
record mechanics live in `internal/platform/kafkaeventbus`. A concrete Kafka
client shim maps the selected client library's producer/consumer record to the
small adapter `Producer`, `Reader`, `Message`, and `Header` interfaces. This
keeps Kafka imports out of Core and out of the public EventBus contract.

## Canonical envelope

The broker-neutral `eventbus.Event` matches
`contracts/events/event-envelope.schema.json` and contains:

- immutable `event_id` and versioned `event_type`;
- UTC `occurred_at`;
- `organization_id` and `workspace_id` tenant context;
- aggregate `entity_type` and `entity_id`;
- source plus correlation/causation/actor/trace metadata;
- a JSON-object `data` payload bounded to 1 MiB.

Runtime decoding is intentionally stricter than generic `encoding/json`: it
rejects unknown envelope fields, missing required nullable fields, duplicate
object keys, non-UTC persisted timestamps, non-object payloads, excess nesting,
and excess payload size. Payload schema validation remains owned by the event's
domain contract; EventBus does not silently mutate or redact a legitimate
business payload.

## Topic and partition policy

For an event type:

`commerce.orders.order_created.v1`

EventBus derives:

`commerce.orders.events.v1`

The first two event-type segments form the event family and the final version
segment becomes the topic version. Examples include
`commerce.catalog.events.v1`, `commerce.orders.events.v1`,
`commerce.inventory.events.v1`, `commerce.social.events.v1`,
`commerce.integration.events.v1`, and `commerce.compliance.events.v1`.

The Kafka partition key is a SHA-256 digest over organization, workspace,
entity type, and entity ID. Therefore one tenant aggregate is stable on one
partition without exposing raw aggregate identifiers in the key. Ordering is
per aggregate, not global.

## Compose topic bootstrap

The Community and single-VPS Compose profiles run a one-shot `kafka-init`
container after the broker health check and before any application process.
It idempotently creates every base topic from `deploy/kafka/topics.txt` plus its
`.retry` and `.dlq` variants. This is explicit rather than relying on broker
auto-creation, which is disabled or restricted in common Kafka deployments.
`TORGNEXA_KAFKA_TOPIC_PARTITIONS` and
`TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR` control the created topic shape;
single-node Community defaults both to `1`, while a production quorum must set a
replication factor supported by its broker count.

## Producer requirements

`kafkaeventbus.NewPublisher` fails closed unless the concrete producer reports:

- idempotent producer semantics enabled;
- acknowledgement from all in-sync replicas (`acks=all`).

The adapter publishes canonical JSON with `application/json`, event ID, and
event type system headers. Producer/client errors are replaced by bounded
adapter errors so broker diagnostics cannot accidentally propagate credentials
or PII through application error text.

Task 008 remains mandatory for database mutations: a request must commit domain
state and the outbox row in the same PostgreSQL transaction, then an outbox
relay invokes this publisher. Direct `DB write -> Publish()` from a transaction
handler is not a substitute for the transactional outbox.

## Transactional outbox

Task 008 makes PostgreSQL the durable hand-off boundary for domain mutations.
A mutating PostgreSQL repository opens one transaction, applies the tenant
scope, writes domain state, and constructs an `outboxrepo.TransactionEnqueuer`
from that **same** `*sql.Tx`. The enqueuer verifies the already-bound
organization/workspace GUCs; it never opens, commits, rolls back, or silently
changes the caller transaction. A mismatch fails closed.

New outbox rows contain both the legacy indexed columns/payload and the full
canonical EventBus envelope. The event ID/body, tenant, aggregate, type, and
creation metadata are immutable. Exact re-enqueue of an existing immutable
event ID is idempotent; reusing an ID with different envelope content is an
error. During the expand phase, pre-Task-008 rows may retain
`event_envelope IS NULL`; the new relay never guesses or publishes those rows.
If an unpublished legacy row exists in a tenant scope, claim fails with an
explicit migration-required error instead of reporting an empty/healthy queue.

The relay is tenant-scoped and lease based:

1. a short PostgreSQL transaction selects ready rows in creation order with
   `FOR UPDATE SKIP LOCKED`;
2. it atomically assigns a cryptographically random lease token/expiry and
   increments the publication attempt;
3. the database transaction commits before any Kafka network call;
4. EventBus publishes the unchanged event;
5. success sets `published_at` only when the event ID + lease token still match
   and the lease has not expired;
6. failure stores only the machine code `publish_failed`, advances
   `available_at` with bounded exponential backoff, and clears the lease.

There is no maximum-attempt discard in the outbox. A committed business event
is authoritative intent and remains retryable until delivery succeeds or an
explicit privileged incident/recovery procedure intervenes. Raw Kafka/client
error strings are never persisted in publication metadata.

A worker crash after claiming causes no loss: another worker can reclaim after
lease expiry. A crash after Kafka publish but before `published_at` can publish
the same immutable event ID again. This is the intentional at-least-once
boundary; Task 009 Inbox/Idempotency must make consumer side effects duplicate
safe. Kafka producer idempotence is useful but does not replace this consumer
inbox guarantee across process crashes and DB/Kafka systems.

`contracts/transport/outbox-publication.schema.json` documents bounded
operational publication metadata. PostgreSQL RLS remains forced per
organization/workspace, application runtime has no DELETE policy, and migration
`000007_transactional_outbox.sql` blocks event-body mutation and hard deletion.

## Delivery, retry and DLQ

Delivery is explicitly **at-least-once**. TORGNEXA does not claim cross-record
exactly-once processing.

Base topics are consumed together with `<base>.retry`. A failed handler returns
a bounded machine classification through `eventbus.Retryable(code)` or
`eventbus.Permanent(code)`. Unknown handler errors are treated as retryable with
the fixed code `handler_error`; raw error strings never enter Kafka headers.

For retryable failure below the budget:

1. calculate bounded exponential backoff;
2. publish the same immutable event ID/body to `<base>.retry` with the next
   attempt, first-observed UTC time, original topic, not-before UTC time, and a
   machine failure code;
3. commit the source offset only after the retry publish succeeds.

A retry consumer waits until `not_before` before invoking the handler. Permanent
failure, invalid envelope/transport metadata, topic/envelope mismatch, or
exhausted retry budget is first published to `<base>.dlq`, then the source
offset is committed. If retry/DLQ publication fails, the source offset is **not**
committed.

A crash after publishing retry/DLQ but before committing the source can create
a duplicate replacement record. That is expected at-least-once behavior and is
why Task 009 Inbox/Idempotency is mandatory before production side effects.

`contracts/transport/event-delivery-metadata.schema.json` documents the retry
and dead-letter metadata shape. DLQ payloads retain the original event for
operator replay and therefore inherit the same tenant/privacy classification,
access controls, retention requirements, and observability redaction rules as
the source topic.

## Consumer integrity checks

Before business code runs, the adapter verifies:

- configured base/retry topic membership;
- unique bounded TORGNEXA system headers;
- `content-type=application/json`;
- event-ID and event-type header presence;
- canonical envelope decoding;
- header values equal envelope values;
- event type maps back to the original base topic;
- retry attempt/first-observed/not-before metadata is valid UTC data.

Poison records are routed to DLQ without invoking the handler.

## Consumer inbox and idempotency

Task 009 completes the consumer-side half of the at-least-once contract. Business
consumers wrap their handler with `postgres/inboxrepo.Processor` and use one stable
logical consumer name such as `orders.projector.v1`. Pod names, Kafka member IDs,
and other ephemeral instance identities are forbidden because they would create a
new deduplication namespace after every restart. If replay semantics intentionally
change, create a deliberately versioned new consumer identity.

The processor opens one PostgreSQL transaction, binds the organization/workspace
scope, and takes a transaction-scoped advisory lock derived from tenant + consumer
+ event ID. The lock only serializes competing deliveries; it is automatically
released on commit, rollback, connection loss, or process crash and therefore needs
no durable lease/cleanup state. After the lock:

1. an existing immutable receipt with the same event type and canonical-envelope
   SHA-256 fingerprint means duplicate success; business code is not invoked and
   Kafka may commit the source offset;
2. the same tenant/consumer/event ID with a different type/fingerprint is a
   fail-closed collision and is mapped to a permanent EventBus failure;
3. if no receipt exists, business PostgreSQL side effects run inside the same
   processor-owned transaction;
4. only after the handler succeeds is the immutable receipt inserted;
5. commit makes the side effects and receipt visible atomically.

A crash or retryable handler failure before commit leaves neither committed
PostgreSQL effects nor a receipt, so the next delivery can safely execute again. A
crash after the database commit but before Kafka offset commit causes redelivery;
the receipt then converts that delivery to duplicate success without repeating the
transactional business effect.

`inbox_receipts` stores only tenant IDs, logical consumer, event ID/type, a SHA-256
fingerprint of the canonical envelope, first-observed/processed timestamps, and the
committed delivery attempt. It deliberately does **not** duplicate the event payload
or arbitrary handler errors. Forced tenant RLS permits only SELECT/INSERT to the
application path, while UPDATE/DELETE/TRUNCATE are blocked and receipts are
immutable. Migration `000008_inbox_idempotency.sql` introduced this shape
additively. After fleet and zero-traffic qualification, contract migration
`000064_retire_legacy_inbox_events.sql` removes the empty pre-Task-009
`inbox_events` placeholder; runtime code continues to use only `inbox_receipts`.

This is not distributed exactly-once. The atomicity guarantee covers PostgreSQL
side effects performed through the inbox-owned transaction. A direct HTTP/provider
call can succeed remotely and then be followed by a local crash before commit; such
external effects must use a provider idempotency key or be represented as a new
transactional outbox event.

`contracts/transport/inbox-receipt.schema.json` documents the durable receipt
metadata. Duplicate/collision counters and receipt age/attempt are observability
signals; raw event payloads and errors must remain outside inbox telemetry.

## Workflow trigger consumer

Task 163 consumes the same canonical events in the stable consumer namespace
`workflow.triggers.v1`. The handler inserts matching published workflow runs and
the Inbox receipt in the same PostgreSQL transaction. `workflow_event_receipts`
is an additional immutable workflow-level mapping used for operator lineage;
the Inbox receipt remains the Kafka redelivery boundary. A duplicate event is a
successful no-op, while a fingerprint collision is permanent poison data.
Only envelope metadata and a SHA-256 input digest enter workflow state; the
event body is never copied to a run, step or evidence row.

## Operational requirements

Kafka is durable transport and replay state, **not transactional business
truth**. PostgreSQL domain/outbox state remains authoritative for a committed
business mutation. Backup/restore therefore preserves Kafka topic definitions,
ACLs, retention and offsets as operational metadata, then reconciles/replays
from authoritative PostgreSQL state.

Production monitoring must expose at least producer errors/latency, consumer
lag, retry rate, retry age, DLQ rate/age, partition skew, under-replicated
partitions, ISR changes, authentication failures, and quota/throttle signals.
Task 066/077 later binds these to SLOs and executable incident runbooks.

Future scheduling remains PostgreSQL scheduler state; Kafka is not a delayed-job
database. The retry topic's bounded `not_before` is only transport failure
backoff metadata.

## Production worker composition (Task 113)

`cmd/worker` now owns the production asynchronous composition root rather than
using the placeholder background wait loop. Startup opens the bounded PostgreSQL
pool and encrypted SecretProvider, constructs a `franz-go` transport behind the
provider-neutral `kafkaeventbus` interfaces, then supervises these long-lived
components under one cancellation context:

1. tenant-safe outbox relay and durable webhook-delivery polling;
2. Kafka base/retry consumption into the durable webhook service;
3. reconciliation lease execution when enabled;
4. S3 quarantine + ClamAV upload-security execution when enabled.

The default webhook consumer subscribes to every transport family/version
present in `contracts/events/event-catalog.json`; a test derives the expected
set from the catalog so a new event family cannot silently be omitted from the
worker defaults. Retry/DLQ semantics remain those defined above.

This does not turn provider packages into hidden infrastructure clients. The
production reconciliation resolver still requires a provider-neutral
`reconciliation.Source`. Until production connector HTTP/source bridges are
implemented, the registry fails closed and the durable job is released for
bounded retry rather than marked complete.
