# OK Reconciliation

The provider returns remote identities as `ok:<group-id>:<topic-id>` and never accepts a status/analytics identity belonging to another configured group.

After a confirmed `mediatopic.post` result, `mediatopic.getByIds` provides read-after-write existence evidence. Analytics reads are advisory snapshots and never mutate canonical Task-020 publication state by themselves.

If a publication-side transport failure, remote 5xx, or OK system error occurs after the write may have reached the provider, the adapter emits `write_outcome_unknown`. The host must stop blind retries and reconcile using retained operation/audit evidence. Task 045 intentionally does not invent an idempotency key unsupported by the audited OK write contract.

Uploaded photo/video objects are provider-side intermediate artifacts. Their IDs/tokens are not TORGNEXA canonical identities and are not persisted as a substitute for the publication remote ID.
