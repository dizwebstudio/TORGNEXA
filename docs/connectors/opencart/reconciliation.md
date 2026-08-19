# OpenCart reconciliation

The bridge API is desired-state. TORGNEXA suppresses already-matching writes and verifies all mutations. Product creates are reconciled by stable SKU. Ambiguous effects return `write_outcome_unknown` unless a follow-up read proves the requested state.
