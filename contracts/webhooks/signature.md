# Webhook Signature Contract v1

TORGNEXA signs the exact UTF-8 request body with HMAC-SHA256 and a per-subscription secret stored only through `SecretProvider` (`webhook_signing`). Plaintext signing material is never persisted in webhook tables or returned by management APIs. Management operations accept 32–64 bytes of signing material.

Canonical signing input:

`<unix_timestamp_seconds>.<raw_body>`

Headers:

- `TORGNEXA-Delivery-Id`: immutable delivery ID; retries reuse the same ID, a manual replay receives a new ID.
- `TORGNEXA-Timestamp`: UTC Unix seconds generated for the individual HTTP attempt.
- `TORGNEXA-Signature: v1=<lowercase-hex-hmac-sha256>`.

Receivers MUST compare signatures in constant time and reject timestamps outside their configured replay window. TORGNEXA's reference verifier defaults to a five-minute replay window.

Rotation is overlap-based. A new secret reference becomes current immediately and the previous reference remains available for a bounded 5-minute to 24-hour overlap. Receivers may validate against current + previous secret during that bounded overlap. Finalization revokes the previous `SecretProvider` reference and clears it from subscription metadata. New deliveries use the current secret; already queued deliveries retain their immutable request snapshot and secret reference.

Signatures cover the raw request body. Re-serializing JSON before verification is not equivalent and MUST NOT be used.
