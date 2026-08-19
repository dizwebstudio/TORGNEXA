# Migration 000020 — durable webhooks

Expand migration introducing `webhook_subscriptions`, durable `webhook_deliveries` and immutable `webhook_delivery_attempts`.

All tables are tenant keyed and forced-RLS. Signing material is represented only by foreign-keyed Task-021 opaque secret references. Initial event enqueue is idempotent per `(organization, workspace, subscription, event_id)` while manual replays receive new delivery IDs. Delivery request identity/body/endpoint/secret snapshot are guarded against mutation; attempt history is insert/select only with rejection triggers for update/delete/truncate.

Claiming uses tenant-scoped `FOR UPDATE SKIP LOCKED` in the PostgreSQL adapter and an opaque compare-by-lease token. The migration stores no remote response body or raw failure text. Binary rollback may leave these additive tables unused; no existing schema is contracted.
