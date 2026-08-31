# ADR-0144 — Редактирование опубликованного сообщения Telegram

Status: Accepted

## Context

The Telegram connector already has a provider adapter for editing a single
remote message, but the application did not expose the operation. Calling the
provider without an immutable publication receipt, approval and durable
idempotency could edit the wrong message or repeat a privileged mutation after
a retry.

## Decision

Admit `social.post.edit` only for the qualified Telegram built-in runtime.
Register
`PATCH /api/v1/social/publications/{publication_id}` as an authenticated,
tenant-scoped and approval-bound operation with a required idempotency key.

The host resolves the active Telegram account and the immutable remote
publication receipt, loads the channel configuration through the existing
secret/config boundary and calls the connector's bounded single-message edit
port. The operation accepts only a result that confirms `published`,
`updated=true` and the exact same remote publication ID. The normalized result
is stored in the existing tenant-scoped operation receipt; raw provider data,
credentials and channel configuration are not persisted there.

Telegram deletion and inbound webhooks remain outside this application
admission until they receive separate authorization, reconciliation and
security contracts.

## Alternatives considered

Calling the connector directly from the UI would bypass the host approval and
tenant boundaries. Treating any successful HTTP response as proof of an edit
would hide provider errors or a mismatched remote identity. Reusing publication
creation would risk creating a second message. These alternatives are
rejected.

## Consequences

Operators can correct one approved published message from the Social surface,
and completed retries are safe. The operation is synchronous at the API host,
while its durable receipt keeps an uncertain provider result pending for later
reconciliation. Connectors without explicit runtime qualification remain
unavailable.

## Security and privacy impact

The capability is write-sensitive. Authentication, tenant resolution,
permission, enabled capability, matching approval, immutable remote receipt,
fixed connector egress and operation idempotency are independent gates. No
bot token, raw provider response or unnecessary channel data enters the
operation receipt.

## Compatibility impact

The additive capability, route and generated SDK method do not alter existing
publication creation, scheduling or receipt semantics. Existing accounts are
unchanged until the capability is enabled.

## Migration and data impact

No database migration is required. The normalized edit result uses the
existing operation receipt store and contains only the remote publication ID,
published status, update acknowledgement and observation time.

## Operational impact

Provider failures are mapped to the existing Social API error surface. A
timeout or unknown result must remain pending until reconciliation; operators
must not blindly retry an unknown mutation. Live qualification requires a
disposable test message and the official
[Telegram Bot API documentation](https://core.telegram.org/bots/api).
