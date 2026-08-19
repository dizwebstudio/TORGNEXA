# CIAN classified/property connector spec

Provider ID: `cian`; family: `classified`; fixed API authority: `public-api.cian.ru`; XML baseline: CIAN Feed v2.

CIAN's current publication boundary is asymmetric. Property advertisements are delivered by an XML file at a stable URL that CIAN retrieves; the Public API is not treated as a publication-write surface. Consequently the runtime manifest intentionally exposes only `classified.publications.status.read`. XML construction is an explicit provider helper, not a fake `classified.publications.write` call.

Authentication material is read only through Task-021 `SecretAccessor` and rendered as `Authorization: Bearer <ACCESS KEY>` at the transport boundary. Non-secret account configuration contains one immutable HTTPS `FeedURL`. Health obtains import-state evidence and requires exact returned-feed equality before the account becomes healthy.

The transport uses typed operations `import.state` and `import.report` rather than embedding an unverified endpoint path in provider code. The live ReDoc page was discoverable during Task 039 qualification but its OpenAPI document could not be fetched in the isolated repository environment. A production HTTP adapter must bind these two operations to the then-current CIAN OpenAPI method names/paths and preserve the fixed `public-api.cian.ru` authority. Fabricating a route is forbidden.

Import evidence is bounded to 8 MiB and normalized into feed URL, order/import ID, processing time, problem flag and aggregate counters. Status reads require both the exact configured feed URL and the exact requested remote import/order ID.

Risk: `classified.publications.status.read` is read-only. Serving the generated XML feed is a host-side publication action and must pass the normal TORGNEXA content/compliance/approval policy before the URL is exposed to CIAN.
