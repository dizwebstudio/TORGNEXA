# Task 131: CBR FX production runtime

## Status

`repository-complete` — production worker composition, repository gates and
live Community runtime qualification passed on 2026-08-26.

## Objective

Remove CBR FX from the planned catalog by composing its already admitted SDK
adapter, immutable FX domain and PostgreSQL store in the production worker and
linking the card to the real Finance surface.

## Deliverables

- [x] bind `cbr-fx` to the official dated Bank of Russia XML endpoint through
  the host-owned DNS/TLS/SSRF-controlled transport;
- [x] cache one daily XML document briefly so a batch does not redownload it
  for every currency;
- [x] run an immediate and six-hourly worker refresh for the reviewed CBR
  currency set through the provider-neutral FX resolver;
- [x] persist immutable rate facts and deterministic resolution evidence using
  the existing PostgreSQL repository;
- [x] make source outages non-fatal to unrelated worker components while
  retaining fail-closed 14-day freshness policy;
- [x] document and configure a conservative Community bridge MTU so TLS egress
  works on lower-MTU VPN/tunnel hosts;
- [x] classify CBR FX as a working separate Finance surface and link its card
  to `/finance`;
- [x] update the integration and FX operating documentation.

## Safety invariants

- no mutable current-rate table, binary float, implicit inversion or hidden
  cross-rate synthesis;
- no rounding of a source observation to force it under the platform decimal
  scale limit; currently IRR/RUB therefore remains explicitly unsupported;
- no connector credential or tenant data is required or persisted;
- no provider-specific branch enters Core or finance domains;
- cache contents never become authority and can be discarded;
- reference-source failure cannot terminate unrelated worker processing.

## Acceptance

- focused and full Go tests/vet pass on Go 1.26.7;
- runtime-support generation and contract/schema checks pass with 38 exact IDs;
- frontend tests/typecheck/build show CBR FX as working in Finance;
- architecture and package-index checks pass;
- rebuilt worker and frontend are healthy, and the worker logs a successful CBR
  refresh with persisted facts visible through `/api/v1/fx/rates`.

## Validation

- full Go test and vet suites: PASS on the pinned Go 1.26.7 validation image;
- contracts, runtime-support generation, architecture and package index: PASS;
- frontend logic tests and production build: PASS;
- live official-source probe: PASS for the dated Bank of Russia XML endpoint;
- rebuilt Community worker: PASS, `worker.fx_reference_refreshed`, 53 rates;
- PostgreSQL evidence: PASS, 53 distinct `base_currency` facts with
  `source_id = cbr` and an effective date of 2026-08-26 Moscow time;
- API and frontend health checks: PASS.
