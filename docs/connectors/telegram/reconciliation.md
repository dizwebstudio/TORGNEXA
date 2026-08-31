# Telegram Reconciliation Notes

Task 041 never writes Social Core state directly.

- canonical Publication identity remains Task-020 `PublicationID`;
- Telegram remote identity is `tg:<chat_id>:<message_id>[,<message_id>...]`;
- the connector does not send `PublicationID` to Telegram because Bot API send methods provide no equivalent idempotency token;
- successful send responses are durable remote receipts for the host;
- ambiguous write transport/5xx is not automatically retried because the remote side effect may already exist;
- exact HTTP 429 `retry_after` remains retryable because it is an explicit provider refusal;
- edit/delete require an exact remote receipt for the configured channel before egress;
- delete success does not delete or rewrite canonical Content/Variant/Publication/audit history;
- verified `channel_post` and `edited_channel_post` updates use a content-addressed delivery ID and host-owned Inbox/outbox deduplication;
- Task 014 remains the owner of drift/reconciliation policy.

Because Bot API has no general `getMessage` operation, Task 041 intentionally does not implement normalized status polling. Reconciliation must use retained receipts, verified inbound update evidence, later canonical actions and operator/provider evidence rather than pretending a status endpoint exists.
