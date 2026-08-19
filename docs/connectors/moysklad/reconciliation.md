# MoySklad Reconciliation

Task 016 supplies read snapshots only. Task 013 owns propagation/idempotency and Task 014 owns drift records and remediation.

Recommended initial ERP policy is remote-authoritative for catalog/inventory/order observations when MoySklad is the operational master. MoySklad `updated` is retained as remote revision evidence; product/order IDs and store/state IDs remain Task-010 remote mappings.

Because Task 016 grants no write capability, reconciliation may detect/notify/route approval but cannot mutate MoySklad. Any future ERP write stage requires its own capability audit, idempotency semantics and approval/risk review.
