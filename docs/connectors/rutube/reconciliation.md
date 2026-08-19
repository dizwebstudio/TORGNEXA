# RUTUBE Reconciliation

Canonical identity is `rutube:<channel-id>:<video-id>`.

After commit, `processing` is a valid durable remote state. Task-014 may poll `ReadSocialPublicationStatus` until `published` or `failed`. A foreign channel identity is rejected before transport access.

Ambiguous create/commit (`write_outcome_unknown`) and ambiguous byte upload (`upload_outcome_unknown`) are intentionally non-retryable Connector SDK conflicts. An operator or transport-specific reconciler must inspect the account-specific official partner contract using the canonical Publication external ID before another upload session is created.

This prevents duplicate videos when the remote side accepted a request but the local caller did not observe the response.
