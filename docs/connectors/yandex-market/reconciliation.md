# Yandex Market Reconciliation

Task 033 supplies read snapshots and inbound notification normalization only. Task 013 owns propagation/idempotency and Task 014 owns drift records/remediation.

Recommended baseline:
- catalog/price/inventory/order remote IDs remain Task-010 mappings;
- page tokens/checkpoints remain opaque remote cursor evidence;
- order notifications are hints that trigger/read fresh canonical remote state rather than becoming a second source of truth;
- duplicate notifications are suppressed through the Task-009 Inbox using the deterministic decoder dedupe key;
- inventory mode is explicit, so reconciliation never compares stocks across guessed warehouse semantics.

Because Task 033 grants no write capability, reconciliation may detect, notify and route approval but cannot mutate Yandex Market. Future write stages require separate capability admission, idempotency semantics, approval/risk classification and audit evidence.
