# Task 134 — Host-owned OAuth refresh runtime

Status: Repository implementation complete

## Problem

Task 106 stored the successful authorization-code response as an encrypted JSON
bundle, but the Connector SDK runtime handed those bytes directly to provider
adapters. Adapters that expected a raw access token could not authenticate, and
no component refreshed an expiring token. Operators therefore had to repeat the
browser authorization flow after access-token expiry.

## Scope

- Project an OAuth account's primary secret to a callback-scoped current access
  token without exposing refresh tokens or client credentials to adapters.
- Refresh authorization-code bundles one minute before expiry through the exact
  manifest token endpoint.
- Serialize refresh-token use across API and worker processes with a tenant- and
  reference-scoped PostgreSQL transaction advisory lock.
- Re-read encrypted material after acquiring the lock and rotate the same opaque
  secret reference to one new immutable ciphertext version.
- Preserve the old refresh token when a provider does not rotate it; atomically
  store a replacement when it does.
- Exchange client-credentials grants just in time without browser interaction or
  long-lived plaintext caching.
- Expose bounded health reasons for refresh failure and required reauthorization.

## Acceptance criteria

- Fresh access tokens do not trigger a refresh or secret rotation.
- Concurrent callers observing one expired bundle perform exactly one remote
  refresh and all receive the rotated access token.
- Missing/rejected refresh material fails closed and never exposes credential
  bytes in errors, logs, audit, events or API responses.
- Provider adapters receive only access-token bytes through `UseSecret`.
- Non-OAuth connector behavior and Connector SDK v1 remain unchanged.
- Go test/vet, contracts, architecture, migration-static and frontend gates pass.

## Explicit exclusions

Task 134 does not admit another connector into the production catalog and does
not reduce the 18 planned entries. Provider-specific installation parameters,
token revocation endpoints and live-provider qualification remain connector
tasks. Task 135 may now compose VK without accepting an unrefreshable bundle.
