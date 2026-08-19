# Migration 000021 — notifications

Expand migration introducing the canonical notification inbox, per-recipient channel preferences, and immutable channel delivery-attempt history.

`notifications` deduplicates by `(organization, workspace, recipient, dedupe_key)`, increments an occurrence counter for repeated conditions, and permits severity to move only upward through application logic. The Web UI reads the inbox directly. External webhook delivery is opt-in and is delegated to Task 063 rather than implementing a second HTTP egress path.

All three tables use explicit organization/workspace keys and forced PostgreSQL RLS. A notification update trigger independently rejects identity/dedupe/first-occurrence mutation, occurrence-count rollback, and severity downgrade. `notification_deliveries` is append-only through privileges and mutation-rejection triggers and stores only bounded machine error codes; remote bodies, headers, tokens and raw provider errors are forbidden.

The migration is additive. Older binaries ignore the new tables, so binary rollback does not require destructive schema rollback.

Retries append a new `(notification_id, channel, occurrence, attempt)` row rather than mutating prior evidence. Attempt numbers are constrained to `1..64` so pathological replay loops fail closed rather than growing history without bound.
