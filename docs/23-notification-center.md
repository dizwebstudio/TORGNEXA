# Notification Center — Task 022

Task 022 implements the canonical tenant-scoped notification inbox and the provider-neutral delivery boundary used by later UI, workflow, connector and compliance features.

## Model

A notification contains a recipient, severity (`info`, `warning`, `critical`), bounded title/body, optional entity link, optional source EventBus identity, a stable deduplication key, occurrence count and read state. PostgreSQL is the system of record.

Deduplication is scoped by `(organization, workspace, recipient, dedupe_key)`. A repeated condition updates the same inbox item and increments `occurrence_count`; it does not repeatedly fan out to channels. Severity is monotonic: a duplicate may escalate, never downgrade. An escalation is a material change and is delivered again. The PostgreSQL update guard independently rejects severity downgrade, occurrence-count rollback, and mutation of notification identity/dedupe/first-occurrence fields.

## Preferences and suppression

Preferences are per recipient and channel with an enable flag and minimum severity. Safe defaults are:

- `web_ui`: enabled, minimum `info`;
- `webhook`: disabled, minimum `warning`.

External delivery is therefore opt-in. A preference that blocks a notification records an immutable `suppressed` delivery outcome rather than silently losing evidence. If a channel is enabled but its provider is not wired, the outcome is `failed/provider_unavailable`, not `suppressed`, so configuration failures cannot masquerade as user preference.

## Providers

`notifications.Provider` is the extension port. Task 022 ships:

- `WebUIProvider`: delivery is satisfied by persistence in the canonical inbox;
- `WebhookProvider`: converts the bounded notification projection into `platform.notifications.notification_created.v1` and delegates to Task 063's durable webhook handler.

The notification subsystem does **not** perform direct HTTP, DNS resolution, signing, retries or DLQ processing. Those concerns remain exclusively owned by Task 063.

## API and authorization

The API exposes inbox listing, idempotent mark-read, delivery-history reads, and preference updates. Tenant scope and recipient identity come only from the authenticated resolver. Requests containing client `recipient_id`, `organization_id` or `workspace_id` selectors are rejected rather than ignored.

Responses are `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

## Persistence and privacy

Migration `000021_notifications.sql` adds `notifications`, `notification_preferences`, and immutable `notification_deliveries`. All tables carry organization/workspace keys and forced PostgreSQL RLS. Delivery evidence stores only normalized channel/status/occurrence/attempt/time and bounded machine error codes; raw provider errors, remote bodies, credentials and headers are not persisted. Attempts are immutable and bounded to 64 per notification/channel/occurrence. Credential-shaped title/body values are rejected at the notification boundary so obvious secrets cannot be copied into the inbox or webhook payload.

Future email, Telegram, MAX, SMS and n8n channels must implement the same provider boundary and preserve the preference, privacy and durable-delivery rules rather than bypassing them.

### Retry and delivery evidence

A distinct repeated condition increments `occurrence_count` but does not fan out again unless severity escalates. A retry of the same source occurrence (same `source_event_id`, or the same occurrence timestamp when no source event is available) is treated as a replay: it keeps the business occurrence count stable and re-attempts provider delivery. Delivery evidence is append-only and records `occurrence` plus monotonically increasing `attempt`; only bounded machine error codes are persisted.
