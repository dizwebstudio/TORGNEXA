# ADR-0104 — Host-owned OAuth refresh runtime

Status: Accepted

## Context

The connector OAuth callback already performs PKCE authorization-code exchange
and encrypts a bundle containing access token, refresh token, expiry and client
registration. The generic Connector SDK runtime nevertheless exposed the whole
bundle through `UseSecret`, while several provider adapters correctly accept
only access-token bytes. No runtime component interpreted expiry or performed a
refresh. This made OAuth connectors unusable after token expiry and encouraged
repeated browser authorization.

API and worker can invoke the same connector account concurrently. Many OAuth
providers rotate or invalidate a refresh token when it is used, so optimistic
secret-version rotation alone is insufficient: two remote refresh requests can
invalidate each other's result before either database update detects a conflict.

## Decision

Add a provider-neutral host token manager between SecretProvider and the frozen
Connector SDK `SecretAccessor`. An account-aware runtime recognizes the primary
OAuth secret from its manifest and supplies only a current access-token byte copy
inside the existing callback. Secondary secret references and non-OAuth accounts
retain ordinary opaque secret behavior.

Authorization-code bundles refresh one minute before expiry. API and worker use
a PostgreSQL transaction-scoped advisory lock keyed by tenant plus secret
reference. After acquiring it, the winner re-reads the latest encrypted bundle;
if another process already refreshed it, no remote call occurs. Otherwise the
host posts `grant_type=refresh_token` with the encrypted client registration to
the exact manifest token endpoint through the existing redirect-free,
DNS-pinned, public-address transport. The resulting complete bundle is rotated
under the same stable secret reference. A provider-returned refresh token
replaces the previous value; omission preserves it.

Client-credentials grants are exchanged just in time from encrypted client
material and are not cached as plaintext. Connector adapters receive only the
resulting access token. Health preparation distinguishes bounded
`oauth_refresh_failed` and `oauth_reauthorization_required` outcomes.

## Consequences

- Browser reauthorization is required only when no refresh token exists or the
  provider rejects/revokes it, rather than on every access-token expiry.
- API and worker cannot race a rotating refresh token across processes.
- Each successful refresh creates one immutable ciphertext version while the
  account's opaque reference remains stable.
- Task 134 changes no connector readiness count; it enables later OAuth-backed
  production compositions such as VK.
- Client-credentials providers incur an exchange per runtime secret use until a
  future encrypted/cache policy is explicitly reviewed.

## Compatibility impact

Connector SDK v1, public REST/OpenAPI, events and plugin contracts are unchanged.
The behavioral contract is corrected: an OAuth provider's primary
`SecretAccessor` callback now receives access-token bytes, never the host's
encrypted bundle representation. Bitrix24 is aligned with this provider-neutral
contract.

## Migration and data impact

No schema migration or backfill is required. Existing `oauth_refresh` secret
versions already contain the required bundle fields. Refresh uses the existing
immutable version rotation and PostgreSQL advisory locks; no lock row or
non-secret token metadata is persisted.

## Security and privacy impact

Access tokens, refresh tokens and OAuth client secrets remain excluded from
normal tables, runtime configuration, logs, errors, events, audit and API
responses. Refresh/client material exists only inside SecretProvider callbacks;
the provider adapter receives a separate access-token byte copy that is wiped
after callback return. Refresh egress is fixed by the reviewed manifest and uses
the existing TLS/public-address/redirect-deny bounds. Lock keys contain only
tenant IDs and opaque secret references.

## Operational impact

Health checks lazily refresh near-expiry credentials before the provider probe.
Workers do the same on first real use. `oauth_refresh_failed` indicates a
temporary token-endpoint/rotation failure; `oauth_reauthorization_required`
means an operator must repeat OAuth. Binary rollback leaves the latest encrypted
version readable but restores the old bundle-exposure defect, so OAuth connector
admission must not roll back independently from this runtime boundary.

## Alternatives considered

Refreshing in each provider adapter was rejected because adapters must not see
client/refresh material or own secret persistence. Valkey locking was rejected
because Valkey is non-authoritative and a lock loss could duplicate a rotating
refresh request. Optimistic secret rotation without a distributed lock was
rejected because it detects the database race only after both remote side
effects. Scheduled blanket refresh was rejected because it increases secret and
network activity for inactive accounts. Returning the whole bundle and asking
every adapter to parse it was rejected as a host-storage leak and incompatible
credential shape.
