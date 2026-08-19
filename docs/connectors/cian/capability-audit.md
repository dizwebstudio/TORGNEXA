# CIAN capability audit

Task 039 admits the smallest verified property-publication slice.

Admitted runtime capability:
- `classified.publications.status.read` — read-only import-state/report reconciliation for the configured feed.

Provider helper, deliberately not a Connector SDK write capability:
- CIAN Feed v2 XML generation for `flatSale` and `flatRent`.

Not admitted:
- API push/create/update/delete of advertisements (publication is XML-feed based);
- `classified.listings.read` because Task 039 did not qualify a stable account-listing read contract;
- `classified.publications.write` because registering/serving a feed URL is not the same as a provider API mutation;
- new-building `newBuildingFlatSale`, suburban, daily-rent and commercial schemas;
- promotion/bid writes, paid-service management, chats/messages, leads/call tracking, statistics and webhooks.

Security/correctness boundary:
- provider API authority is fixed to `public-api.cian.ru`;
- access key exists only in the secret callback and is never returned in errors;
- account health/status are bound to the exact configured feed URL;
- a status read is additionally bound to the exact remote import/order ID;
- XML object count, output bytes, text, coordinates, areas, photos, phones and identifiers are bounded;
- TORGNEXA requires HTTPS for the feed and photo URLs, stricter than the provider's general HTTP feed allowance;
- unsupported property categories fail closed rather than emitting partial XML.
