# Auto.ru classified/vehicle connector spec

Provider ID: `auto-ru`; family: `classified`; fixed authority: `apiauto.ru`; API baseline: `1.0`.

Authentication material is read only through Task-021 `SecretAccessor`. One secret contains the Auto.ru `x-authorization` token and, when provisioned, `x-session-id`. Non-secret account configuration contains the numeric Auto.ru `account_id` and optional `x-dealer-id`. Health calls `GET /1.0/dealer/account`, accepts provider scalar account IDs, requires exact configured-account equality and reports non-`ACTIVE` accounts as degraded.

Baseline remote operations:
- listing read: `GET /1.0/user/offers/cars` with bounded page/page_size;
- vehicle feed submit: `POST /1.0/feeds/tasks/cars/{NEW|USED}` for an HTTPS XML source;
- feed task status: `GET /1.0/feeds/history/{task_id}`.

`classified.listings.read` and `classified.publications.status.read` are read risk. `classified.publications.write` is `write_sensitive`. Auto.ru feed submission has no qualified TORGNEXA-controlled remote idempotency key in this baseline; therefore a transport failure or write-side 5xx after dispatch becomes non-retryable `write_outcome_unknown`.

The Connector SDK request is provider-neutral: publication kind, section and source URL. Auto.ru-specific XML construction lives under `connectors/classified/auto-ru` rather than Core.
