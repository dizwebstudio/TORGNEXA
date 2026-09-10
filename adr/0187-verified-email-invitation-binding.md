# ADR-0187 — Verified email ownership for invitation binding

Status: Accepted

## Context

Audit A02 found that workspace membership resolution accepted a profile email
without ownership evidence. A valid OIDC identity able to choose an unverified
address could claim a matching unbound invitation and its workspace role. The
same profile helper could also combine UserInfo and token fallback values, which
must not be used to combine email ownership assertions across sources.

## Decision

Preserve ADR-0083's authentication → tenant resolution → authorization sequence.
Authenticate through the configured, bounded UserInfo endpoint and retain the
existing issuer/client/subject/expiry/session checks. Only a literal boolean
`email_verified: true` paired with the email in that same authenticated UserInfo
response provides invitation ownership evidence. Its subject must match the
validated bearer identity. Normalize the address using the existing invitation
case/whitespace rules and accept only a bounded mailbox without a display name.

Keep profile email separate from `Principal.VerifiedEmail`. Profile fallback
values and decoded token `email_verified` claims never establish invitation
evidence. If UserInfo omits the email, its verification flag cannot validate a
fallback email from the token. False, null and absent verification leave the
proof empty. Incorrect JSON types fail authentication instead of being coerced.

The repository accepts an explicit `MemberIdentity` rather than two ambiguous
string arguments. Existing active membership resolves by issuer-bound subject
without requiring an email claim. Only a matching, unbound `invited` row can
bind with `VerifiedEmail`; the existing transaction and unique constraints
provide atomic binding. The authorization lookup supplies no email proof and
cannot accept invitations. Workspace roles remain database-authoritative.

## Compatibility and rollout

This intentionally tightens authentication prerequisites for unbound invitees.
The public v1 payloads, routes, status codes and SDK signatures do not change;
OpenAPI documents the ownership requirement and generated source hashes update.
An authenticated, unbound caller without proof receives the normal 403 and the
invitation is unchanged. Already bound active members keep logging in without
verified email. Disabled members stay denied. The explicit development-only,
empty-workspace administrator bootstrap remains separate and unchanged.

Before replacing API instances, configure the trusted provider to expose the
actual email and boolean ownership result in UserInfo. A token-only mapper is
insufficient. Do not replace missing ownership evidence with a hard-coded true
claim. Deploy the patched API to every replica; old instances still accept the
old policy during a mixed-version window. No SQL migration is required.

Existing bindings do not contain historic email-verification evidence, so the
upgrade cannot automatically decide which were legitimate. Operators should
review bindings made under the previous policy and use the existing governed
disable/session-revocation procedures for confirmed suspicious identities.
There is no automatic role removal or rewrite of append-only evidence.

## Security and privacy

VerifiedEmail is bounded identity data retained only in request memory, excluded
from JSON serialization, public contracts, logs, audit payloads and events.
No persisted field or retention path is added. Error responses do not reveal
whether an invitation exists. Frontend claims, headers and profile updates
cannot supply this proof. Correct upstream email-verification behavior remains
part of the configured IdP trust boundary; this change does not claim protection
against a compromised provider or replace the separate JWT/JWKS work in 234.6.

## Validation

Regression tests use a local TLS UserInfo fixture accepting one exact synthetic
credential, the actual OIDC authenticator/security composition and disposable
PostgreSQL with forced RLS and an unprivileged application role. Cases include
true/false/missing/null/wrong-type flags, address and subject mismatches,
token-only fallback, attempted admin-role acquisition, existing/disabled
membership, cross-workspace lookup, unchanged rejected invitations and
successful idempotent binding. Race detector and the existing A03–A08 database
regressions run in the same reproducible gate.
