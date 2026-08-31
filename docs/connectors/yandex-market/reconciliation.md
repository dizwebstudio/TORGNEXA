# Yandex Market Reconciliation

Task 033 supplies read snapshots and inbound notification normalization. Task
116 adds exact price writes. Task 172 adds exact inventory writes through the two documented
Yandex Market stock-update variants; both return provider acceptance only and
rely on a later snapshot for convergence. Task 013 owns propagation/idempotency
and Task 014 owns drift records/remediation.

Recommended baseline:
- catalog/price/inventory/order remote IDs remain Task-010 mappings;
- page tokens/checkpoints remain opaque remote cursor evidence;
- order notifications are hints that trigger/read fresh canonical remote state rather than becoming a second source of truth;
- duplicate notifications are suppressed through the Task-009 Inbox using the deterministic decoder dedupe key;
- inventory mode is explicit, so reconciliation never compares stocks across guessed warehouse semantics.

Remote acceptance of a price or inventory write is not proof that the catalogue
has already converged. The connector returns `Applied=true`, `Reconciled=false`;
a later price or inventory snapshot confirms the desired value or records drift
for remediation. Product and order-status drift remains detect/notify/approval-only.
Any further write stage requires separate capability admission, idempotency
semantics, approval/risk classification and audit evidence.
