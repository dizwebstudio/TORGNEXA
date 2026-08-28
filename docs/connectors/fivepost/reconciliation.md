# Reconciliation

5Post status responses are remote-authoritative observations. A future host
bridge must persist only the canonical remote shipment identifier, normalized
status and observation timestamp, then reconcile by polling according to the
partner contract. Ambiguous create/cancel outcomes must be probed before a
retry, and duplicate commands must retain the same idempotency key.

The current application surface executes only the authenticated credential
probe. It does not schedule shipment reconciliation or issue write commands;
those operations remain qualification-gated until a current partner contract
and replay-safe fixtures are admitted.
