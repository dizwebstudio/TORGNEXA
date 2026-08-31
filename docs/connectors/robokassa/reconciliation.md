# Reconciliation

Robokassa remote status is authoritative. TORGNEXA stores correlation/evidence
and compares local projections with remote observations via
`GetInvoiceInformationList`; ambiguous outcomes (State.Code values other than
100 "credited" or 10 "cancelled") stay `pending` rather than being guessed.
Refund creation is asynchronous: the provider `requestId` is stored as the
remote refund ID and the local lifecycle remains `accepted` until a later
refund-state/reconciliation bridge observes completion. A timeout after the
refund POST is `unknown`, never a blind retry.
