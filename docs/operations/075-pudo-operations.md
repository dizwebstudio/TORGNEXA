# Task 075 — PUDO operations

PUDO supports own and external pickup points, bounded capacity and the lifecycle:
`created -> arrived -> ready -> issued`, or `ready -> expired -> return_pending -> returned`.

Creation checks point capacity atomically in the reference service. Issue/return transitions expose hooks for payment/fiscal integration and a reporting sink. Expiry reconciliation moves overdue ready orders to `expired`; event history is append-only in PostgreSQL. External directory synchronization later uses Task 074/090 pickup capabilities without changing this state machine.
