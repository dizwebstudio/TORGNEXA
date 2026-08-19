# Task 106: Connector Authentication Flow

## Status
`implemented`

## Objective
Implement manifest-driven API-key/OAuth/certificate connection setup and reauthorization with validation before activation.

## Dependencies
105, 025, 029

## Acceptance
- auth forms are selected from manifest auth requirements, not provider-name branches;
- OAuth state/PKCE/callback binding is replay-resistant;
- secret values never return to the browser after submission;
- connection tests use bounded timeouts and safe error mapping;
- failed validation leaves the account inactive.

## Implementation
- Direct API-key, bearer, basic and certificate material is accepted only through the authenticated tenant boundary, encrypted with the Community `SecretProvider`, and replaced through revoke-after-bind rotation.
- Manifest v2 declares OAuth grant/endpoints/scopes/client authentication plus bounded credential-aware `connection_test` requests. Production code does not dispatch on provider names.
- Authorization-code flows use cryptographic state and PKCE S256. The exact deployment callback, tenant, actor, account version and ten-minute expiry are persisted with a SHA-256 state digest; raw state/verifier exist only in an encrypted `oauth_state` secret. Consumption is atomic and one-time.
- OAuth client registration and token bundles are encrypted and never returned. Successful callback still leaves the account disabled with unknown health; failed exchange records safe unavailable health and cannot activate the account.
- Connection checks perform a redirect-free, DNS-pinned, private-address-denying HTTPS request with a maximum 15-second timeout and 64-KiB response bound. Only normalized health reason codes are persisted or returned.
- The frontend renders authorization-code and client-credentials actions from generated manifest metadata and completes callbacks through the authenticated API before returning to the integration card.
