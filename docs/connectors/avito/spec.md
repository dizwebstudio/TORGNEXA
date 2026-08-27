# Avito classified connector spec

Provider ID: `avito`; family: `classified`; authority: `api.avito.ru`.

Authentication is the current OAuth2 access token supplied only through Task-021 `SecretAccessor`; Task 134 owns encrypted bundle parsing, expiry refresh and rotation outside this provider. Account configuration contains the numeric Avito `user_id`; health calls `GET /core/v1/accounts/self` and requires an exact match before the account is healthy.

Baseline remote operations:
- listing read: `GET /core/v1/items`;
- leads/chats: `GET /messenger/v2/accounts/{user_id}/chats`;
- message history: current Messenger V3 read surface;
- reply: `POST /messenger/v1/accounts/{user_id}/chats/{chat_id}/messages`;
- listing stats: `POST /stats/v1/accounts/{user_id}/items`.

Read surfaces are risk `read`. `classified.messages.reply` is `write_sensitive`; callers must pass TORGNEXA authorization/approval policy before dispatch. Because the send endpoint provides no qualified caller idempotency key in this baseline, an ambiguous transport/5xx write result is `write_outcome_unknown` and is not automatically retried.
