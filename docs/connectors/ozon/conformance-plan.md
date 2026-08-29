# Ozon conformance plan

Task 012 is the second admitted marketplace provider after the provider-neutral Connector SDK, plugin security, sandbox/dry-run and conformance gates are already repository-complete.

## Required machine evidence

Canonical report: `docs/connectors/ozon/conformance-report.json`.

The provider adapter runs all thirteen Task-064 checks through the shared `conformance.SandboxFixture`; Ozon-specific semantic tests remain in `connectors/marketplaces/ozon`.

## Semantic fixture coverage

- executable manifest equals committed JSON and remains read-only;
- strict two-part `Client-Id`/`Api-Key` credential bundle;
- health uses current `/v3/product/list` and normalizes auth/rate/service failures;
- product list + info composition validates one-to-one product/offer identity;
- `last_id` is opaque, bounded and replayed without host interpretation;
- malformed/duplicate/missing product data and response drift fail closed;
- warehouse list rejects duplicates and partial pagination;
- stock selection uses bounded `offer_id`, subtracts reserved from present, represents missing warehouse stock as zero and rejects unsafe/partial rows;
- raw remote bodies and raw transport errors never escape normalized errors;
- provider imports remain inside the Connector SDK boundary.

## Live qualification

Repository qualification is deterministic and offline. Before enabling a real seller account, run a least-privilege smoke test with a dedicated Ozon key: health, one bounded product page/details call, warehouse list and bounded FBS stock read. Do not retain production credentials or seller payloads as conformance evidence.
