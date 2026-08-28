# Reconciliation

Robokassa remote status is authoritative. TORGNEXA stores correlation/evidence and compares local projections with remote observations via `GetInvoiceInformationList`; ambiguous outcomes (State.Code values other than 100 "credited" or 10 "cancelled") stay `pending` rather than being guessed, and are reconciled instead of silently retried.
