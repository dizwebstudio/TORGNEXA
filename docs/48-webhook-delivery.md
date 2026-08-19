# Durable Webhook Delivery Service — Task 063

Outbound webhooks are a first-class durable capability used by n8n and external systems. The service consumes canonical EventBus events and creates one durable delivery per matching active subscription. The EventBus remains at-least-once, and `(tenant, subscription, event_id)` uniqueness makes duplicate EventBus delivery idempotent at initial enqueue.

## Tenant and secret boundary

Subscriptions, deliveries and history are always organization/workspace scoped. API scope comes only from authenticated server-side identity; request bodies containing tenant identifiers are rejected by strict JSON decoding. PostgreSQL repeats tenant predicates and all three webhook tables use forced RLS.

Signing keys are `SecretProvider` references with class `webhook_signing`. Webhook tables never contain plaintext keys. Subscription responses omit secret references. Signing material is accepted only at create/rotation boundaries, used through `SecretProvider.Use`, and never written to logs, response history or audit summaries.

## Delivery model

The durable request snapshot contains delivery ID, event ID/type, tenant IDs, canonical event timestamp and event `data`. Endpoint URL and signing-secret reference are snapshotted when the delivery is queued. Delivery identity/body/config snapshot are immutable in PostgreSQL; only operational state may advance.

A worker claims one tenant-scoped row using `FOR UPDATE SKIP LOCKED`, increments the one-based attempt number and installs an opaque lease token. A stale worker cannot complete a delivery after another worker has reclaimed it because completion compares both attempt and lease token.

Default retry policy is exponential `1s, 2s, 4s, ...`, capped at 15 minutes with eight total attempts. Network failures, HTTP 408/425/429 and 5xx are retryable. Other non-2xx statuses are permanent and go directly to DLQ. Exhausted retry budget also goes to DLQ. Repeated permanent endpoint failures increment a subscription counter and disable the endpoint after five consecutive permanent failures; a successful delivery resets the counter.

Every completed attempt appends immutable history containing only outcome, HTTP status, duration and bounded machine error code. Response bodies, raw network errors, headers and credentials are never persisted.

## Replay and rotation

Manual replay creates a new delivery ID and `replay_of` link, updates `delivery_id` inside the canonical JSON body, and uses the subscription's current endpoint/signing secret. The source attempt history remains immutable.

Secret rotation activates a new `SecretProvider` reference and retains the previous reference for a caller-selected overlap of 5 minutes to 24 hours. Finalization after the overlap revokes the old reference. See `contracts/webhooks/signature.md`.

Lifecycle disable is idempotent and evidence-preserving. `DELETE /webhook-subscriptions/{id}` changes an active subscription to `disabled`, prevents future matching deliveries and revokes current/previous signing-secret references. The subscription row and immutable delivery/attempt history are retained; this endpoint exists so external automation runtimes such as n8n can cleanly deactivate workflow-owned subscriptions without hard deletion.

## SSRF and egress policy

Endpoints are HTTPS-only, port 443 by default, and cannot contain userinfo, query strings or fragments. Endpoint DNS is resolved on every send. Literal/resolved loopback, private, link-local, carrier-grade NAT, documentation/test, multicast and reserved ranges are rejected. If a hostname resolves to a mixture of public and blocked addresses the entire endpoint fails closed.

The reference HTTP transport dials a validated resolved IP while retaining the original hostname for HTTP Host/TLS SNI. Redirects are disabled so a public endpoint cannot redirect a worker into an internal address. DNS is revalidated for every attempt, which prevents a stored hostname from bypassing the policy after rebinding.

## API

Management surfaces:

- `GET/POST /api/v1/webhook-subscriptions`;
- `DELETE /api/v1/webhook-subscriptions/{id}` (disable + revoke signing material, retain evidence);
- `POST /api/v1/webhook-subscriptions/{id}/rotate-secret`;
- `POST /api/v1/webhook-deliveries/{id}/replay`;
- `GET /api/v1/webhook-deliveries/{id}/history`.

All responses are `no-store`/`nosniff`. Request JSON is bounded and rejects unknown/trailing fields.

## Operational qualification

Repository completion proves contracts, queue/lease state transitions, signature/replay behavior, backoff/DLQ, rotation and SSRF policy. Production must still qualify DNS resolver/network policy integration, request timeout sizing, worker concurrency, queue age/retry SLOs and alerting against the deployment topology. Task 066 owns broader performance/SLO qualification.
