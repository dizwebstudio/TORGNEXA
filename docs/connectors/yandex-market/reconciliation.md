# Yandex Market Reconciliation

Task 033 supplies read snapshots and inbound notification normalization. Task
116 adds exact price writes only. Task 013 owns propagation/idempotency and Task
014 owns drift records/remediation.

Recommended baseline:
- catalog/price/inventory/order remote IDs remain Task-010 mappings;
- page tokens/checkpoints remain opaque remote cursor evidence;
- order notifications are hints that trigger/read fresh canonical remote state rather than becoming a second source of truth;
- duplicate notifications are suppressed through the Task-009 Inbox using the deterministic decoder dedupe key;
- inventory mode is explicit, so reconciliation never compares stocks across guessed warehouse semantics.

Remote acceptance of a Task-116 price write is not proof that the catalogue has
already converged. The connector returns `Applied=true`, `Reconciled=false`; a
later price snapshot confirms the desired value or records drift for remediation.
Product, inventory and order-status drift remains detect/notify/approval-only.
Any further write stage requires separate capability admission, idempotency
semantics, approval/risk classification and audit evidence.
