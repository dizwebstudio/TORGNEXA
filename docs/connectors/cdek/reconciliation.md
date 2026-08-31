# Reconciliation

CDEK remote status is authoritative. TORGNEXA stores correlation/evidence and compares local projections with remote observations; ambiguous outcomes fail closed and are reconciled instead of silently retried.

An accepted refusal is correlated to the original CDEK order UUID/number. A
timeout after the refusal request is recorded as an unknown carrier outcome;
the worker must not submit a second refusal automatically. Subsequent tracking
or order history is the source of truth for the final carrier state.
