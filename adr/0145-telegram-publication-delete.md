# ADR-0145 — Удаление опубликованного сообщения Telegram

Status: Accepted

## Context

The Telegram connector already implements bounded deletion for one message or
an album, but the application did not expose that provider operation. Remote
deletion is irreversible, so an unscoped call or a retry after an unknown
provider result could remove the wrong message or repeat a privileged action.

## Decision

Admit `social.post.delete` only for the qualified Telegram built-in runtime.
Register
`DELETE /api/v1/social/publications/{publication_id}` as an authenticated,
tenant-scoped and approval-bound operation with a required idempotency key.

The host resolves a published local publication, its active Telegram account
and the immutable remote publication receipt, then calls the connector's
bounded single-message delete port. The operation accepts only a result that
confirms `deleted=true` for the exact same remote publication ID. The
normalized result is stored in the existing tenant-scoped operation receipt;
raw provider data, credentials and channel configuration are not persisted
there.

The local publication remains an immutable record of the publication attempt;
the API result is the authoritative receipt of the external deletion. A
future local lifecycle state, if required, needs a separate core decision.
Telegram inbound webhooks remain outside this admission.

## Alternatives considered

Calling the connector directly from the UI would bypass host approval and
tenant boundaries. Treating any successful HTTP response as proof of deletion
would hide a false provider acknowledgement. Reusing a shipment-cancellation
or local publication-status route would conflate external deletion with a
different lifecycle. These alternatives are rejected.

## Consequences

Operators can remove one approved published message from the Social surface,
and completed retries are safe. The operation is irreversible at Telegram;
ambiguous outcomes remain pending for reconciliation and must not be blindly
retried. Connectors without explicit runtime qualification remain
unavailable.

## Security and privacy impact

The capability is write-sensitive. Authentication, tenant resolution,
permission, enabled capability, immutable remote receipt, matching approval,
fixed connector egress and operation idempotency are independent gates. No
bot token, raw provider response or unnecessary channel data enters the
operation receipt.

## Compatibility impact

The additive capability, route and generated SDK method do not alter existing
publication creation, editing or scheduling. Existing accounts are unchanged
until the capability is enabled.

## Migration and data impact

No database migration is required. The normalized deletion result uses the
existing operation receipt store and contains only the remote publication ID,
deletion acknowledgement and observation time.

## Operational impact

Provider failures are mapped to the existing Social API error surface. A
timeout or unknown result must remain pending until reconciliation. Live
qualification requires a disposable test message and the official
[Telegram Bot API documentation](https://core.telegram.org/bots/api).
