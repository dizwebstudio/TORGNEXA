# Instagram Reconciliation Notes

- canonical publication identity remains Task-020 `PublicationID`;
- successful remote identity is `instagram:<ig_user_id>:<media_id>`;
- status reads reject a remote ID belonging to a different configured Instagram user before egress;
- Task-020 remains canonical owner of publication lifecycle and audit evidence;
- container IDs and signed staging URLs are provider-local transient data and are not canonical content identifiers;
- ambiguous POST outcomes fail closed as `write_outcome_unknown`; operator/reconciliation logic must establish whether the remote side effect occurred before another publish attempt.

Task-014 remains owner of drift/reconciliation policy.
