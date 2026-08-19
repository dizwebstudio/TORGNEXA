# Avito reconciliation

Listings, chats/messages and statistics are remote observations and remain linked through Task-010 mapping. Periodic reads reconcile status/counters without inventing local remote revision data. A failed or ambiguous message reply is not retried automatically; the operator/reconciliation path must inspect the chat before resubmission.
