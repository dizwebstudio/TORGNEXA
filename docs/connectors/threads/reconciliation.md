# Threads Reconciliation Notes

- canonical publication identity remains Task-020 `PublicationID`;
- remote identity is `threads:<threads_user_id>:<media_id>`;
- status reads reject foreign configured user IDs before egress;
- Task-088 remains authoritative for media release; staging URLs are transient provider transport material;
- token rotation preserves the same opaque Task-021 secret reference and never stores plaintext in provider state;
- ambiguous write outcomes fail closed; Task-014/operator recovery must resolve remote state before another write attempt.
