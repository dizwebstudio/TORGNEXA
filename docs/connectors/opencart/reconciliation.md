# OpenCart reconciliation

The bridge API is desired-state. TORGNEXA suppresses already-matching writes and verifies all mutations. Product creates are reconciled by stable SKU. Ambiguous effects return `write_outcome_unknown` unless a follow-up read proves the requested state.

Order status writes use the configured installation-specific `order_statuses`
map and are read before and after the bridge mutation. Missing, duplicate,
non-numeric or zero status IDs fail closed before any remote call.
