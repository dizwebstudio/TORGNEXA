# VK Reconciliation Notes

Task 040 never writes Social Core state directly.

- canonical Publication identity remains Task-020 `PublicationID`;
- adapter remote publication identity is `-<group_id>_<post_id>`;
- publication retries reuse `PublicationID` as VK `guid`;
- remote status reads verify the configured group before network access;
- missing wall post maps to explicit `remote_missing` evidence, not implicit deletion of canonical content;
- comments retain remote IDs only in the engagement projection;
- post reach is observational analytics and cannot mutate canonical Publication status.

Task 014 remains the owner of drift/reconciliation policy. Provider-specific remote IDs stay outside Core schemas and are suitable for the existing EntityMapping boundary when durable reverse lookup is required.
