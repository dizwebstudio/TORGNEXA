# ADR-0108 — OAuth per-tenant host template

Status: Accepted

## Context

The host-owned OAuth runtime (ADR-0104/Task 134) resolves
`authorization_url`/`token_url` straight from the manifest's
`sdk.OAuth2Configuration`: one fixed URL per connector, used identically for
every tenant. Every OAuth2 connector admitted so far (VK, Bitrix24) fits
this shape — Bitrix24's portal-specific OAuth even routes through a single
centralized broker host (`oauth.bitrix.info`) that resolves the correct
portal internally, so a fixed URL was never actually a limitation before now.

Shopify has no such broker: its authorization and token endpoints are
literally per-merchant (`https://{shop}.myshopify.com/admin/oauth/...`).
Admitting it as a full OAuth2 connector therefore requires the host itself,
not just the authorization code, to vary per tenant account — something the
generic `oauthStart`/`oauthCallback` handlers in
`internal/app/api/connector_accounts.go` had no way to express without
hardcoding a Shopify-specific branch into otherwise fully provider-neutral
code, which would violate this repository's Core/API provider-neutrality
rule the same way a hardcoded connector-ID switch would anywhere else.

Two smaller, unrelated gaps surfaced from the same investigation: Shopify's
authorize endpoint requires comma-joined scopes (RFC 6749's default is
space-joined, which every existing connector already relies on and must not
change), and Shopify's token exchange needs one static extra form field
(`expiring=1`) to receive a rotatable refresh token instead of a legacy
non-expiring one.

## Decision

Add four additive, opt-in fields to `sdk.OAuth2Configuration`:
`HostParameter` (names the non-secret runtime-config key holding the
tenant's host), `HostSuffix` (the resolved host must end with this literal
suffix — the only thing standing between a tenant's own runtime config and
an attacker redirecting the OAuth flow to an arbitrary host), `ScopeSeparator`
(defaults to space, unchanged for every existing connector), and
`ExtraTokenParams` (static manifest-declared token-exchange form fields,
validated against a reserved-key denylist so they can never shadow
`client_id`/`code`/etc). When `HostParameter` is empty — every connector's
manifest today — `Validate()` takes the exact same code path it always has.

`connectorauth.ResolveOAuth2Host(configuration, hostConfig)` performs the
substitution: it is a no-op unless `HostParameter` is set, and when it is,
the manifest's `authorization_url`/`token_url` must literally equal
`https://{host}` plus an optional path — a template shape enforced by
`Validate()`, not free text. `oauthStart`/`oauthCallback` call it exactly
once, gated only on `configuration.HostParameter != ""`, which is manifest
data, not a connector-identity branch — the generic OAuth HTTP layer stays
exactly as provider-neutral as it was before this ADR.

## Consequences

- Shopify (and any future per-tenant-host OAuth2 provider) can be admitted
  without a provider-specific branch in `internal/app/api`.
- Every existing OAuth2 connector's manifest, behavior and test coverage is
  byte-for-byte unchanged: `HostParameter`/`HostSuffix`/`ExtraTokenParams`
  are absent from their manifests, and `ScopeSeparator` defaults to the
  space this repository's existing connectors already depend on.
- A manifest author declaring `HostParameter` must also declare a correct
  `HostSuffix`; getting the suffix wrong is a manifest review defect, not a
  runtime one — `Validate()` rejects an empty or malformed suffix outright.

## Compatibility impact

Connector SDK v1's frozen `Connector`/`Runtime` roots are unchanged; this
extends only the non-secret `OAuth2Configuration` port additively. Public
REST/OpenAPI, events and plugin contracts are unaffected — `oauth-start`/
`oauth-callback` request/response shapes are identical for every connector.

## Migration and data impact

No schema migration. The resolved host is read from the same non-secret
per-account runtime-config store (`connectorconfigrepo`) every
`runtime_config_template`-declared connector already uses (onec, WooCommerce,
Bitrix24's portal_host, ...); no new storage concept is introduced.

## Security and privacy impact

`ResolveOAuth2Host` never trusts a resolved host on its own: it must match a
DNS-hostname shape, be strictly longer than `HostSuffix`, and end with that
exact literal suffix, and the fully-substituted URL is re-validated through
the ordinary `validHTTPSURL` check `Validate()` already applies to every
fixed-host connector. A tenant's own runtime config therefore cannot direct
the OAuth authorize/token flow anywhere outside the declared provider
domain, closing the same class of SSRF/open-redirect risk the pinned-dial
transport already closes for ordinary API calls. `ExtraTokenParams` values
are static manifest data reviewed the same way any other manifest field is,
never tenant- or request-supplied, and cannot name a reserved OAuth
parameter.

## Operational impact

None: this is inert until a manifest sets `HostParameter`, which today is
only Shopify's. No existing connector's OAuth flow, health check, or refresh
behavior (ADR-0104) changes.

## Alternatives considered

Hardcoding a Shopify-specific host-substitution branch directly in
`oauthStart`/`oauthCallback` was rejected: it would be exactly the
provider-identity branch in Core/API this repository's architecture policy
forbids, and would need to be repeated for every future per-tenant-host
provider. Requiring a centralized OAuth broker the way Bitrix24 has was
rejected because Shopify has no such broker and building TORGNEXA's own
would mean operating a public redirect service, a materially larger and
riskier surface than a bounded string-template substitution. Allowing an
unbounded/unsuffixed tenant-supplied host was rejected as an open redirect
and SSRF risk with no compensating control.
