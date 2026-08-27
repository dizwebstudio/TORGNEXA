# ADR 0101: CBR FX production runtime

Status: Accepted

## Context

Task 089b delivered the immutable FX domain, PostgreSQL evidence store,
provider-neutral `FXRateReader` adapter and the `cbr-fx` connector package, but
the production API/worker composition did not instantiate them. As a result,
the Finance page could list persisted facts but no production component
obtained them, and the runtime-truthful catalog correctly kept CBR FX planned.

## Decision

Extend the ADR-0090 built-in composition boundary with one host-owned CBR
transport bound to the official explicitly dated daily XML endpoint. The
transport keeps a bounded 15-minute process cache of the whole daily document
because the provider-neutral SDK reads one ordered currency pair per call.
The cache is only an egress optimization; PostgreSQL facts remain authoritative.

Compose an FX reference component in the production worker. On startup and
every six hours it resolves the reviewed CBR foreign-currency/RUB set through
the existing `fx.Resolver`, persists immutable facts and resolution evidence,
and applies a 14-day fail-closed freshness ceiling. A temporary reference-source
failure is logged with a bounded error code and retried on the next interval;
it does not stop unrelated tenant, webhook, reconciliation or upload work.

Set the Community backend bridge MTU from
`TORGNEXA_DOCKER_NETWORK_MTU` (default 1376). This prevents TLS handshake
blackholes on VPN/tunnel hosts whose egress path MTU is lower than the Docker
bridge default. The value is deployment topology, not provider behavior.

Classify `cbr-fx` as `separate_surface: finance`, not as generic product sync.
The integration card links to the existing Finance → FX rates page and cannot
create a misleading tenant connector account. No authentication is required.

## Consequences

CBR FX becomes genuinely operational without acquiring generic product-sync
semantics. Adding another reference source remains an explicit composition and
precedence-policy change. The short-lived whole-document cache reduces remote
load but cannot satisfy a lookup without persisted and freshness-checked facts.

## Compatibility impact

Connector SDK v1, public REST paths, event schemas and database schema are
unchanged. The runtime-support schema additively admits the `finance` surface;
generated TypeScript consumers receive that additional literal value.

## Migration and data impact

No migration or backfill is required. Existing append-only FX tables are
populated through their published repository interface. Repeated stable facts
are idempotent and conflicting immutable content remains rejected.

## Security and privacy impact

The source is public read-only reference data and contains no tenant data or
credentials. Egress is HTTPS-only, DNS-resolved to public addresses, pinned for
the request and redirect-disabled by the common built-in transport. Core and
finance packages contain no provider-name branch.

## Operational impact

Operators see CBR FX as working in Finance. The worker performs an immediate
refresh after start, so an empty installation obtains facts without a manual
API mutation. Source outages retain the last immutable observations while
freshness policy continues to reject observations older than 14 days.
Changing bridge MTU requires recreating the Compose network and containers but
does not remove named volumes.

## Alternatives considered

Creating a generic tenant connector account was rejected because CBR rates are
global public reference data and have no tenant credentials or product-sync
direction. Fetching rates in the browser/API read request was rejected because
GET must not cause hidden persistence and client availability must not control
finance reference history. Downloading the daily XML once per currency was
rejected as unnecessary remote load; a bounded non-authoritative document cache
preserves the pair-oriented SDK contract.
