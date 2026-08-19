# MAX Reconciliation Notes

Task 042 never writes Social Core state directly.

- canonical publication identity remains Task-020 `PublicationID`;
- MAX remote identity is `max:<chat_id>:<mid>`;
- successful `POST /messages` response is retained by the host as remote receipt;
- `GET /messages/{mid}` provides bounded read-after-publish status and always re-checks the configured channel;
- ambiguous send transport/5xx is not automatically replayed because no audited caller idempotency token is available;
- inbound updates are independently authenticated and content-addressed before host Task-009 dedup;
- provider webhook retries must therefore converge on the same delivery ID even if JSON key order/whitespace changes;
- canonical Content/Variant/Publication/audit history is never deleted by provider actions;
- Task 014 remains the owner of drift/reconciliation policy.

If a send outcome is unknown, reconciliation/operator recovery must resolve the remote side effect before another publish attempt is authorized.
