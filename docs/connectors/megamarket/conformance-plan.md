# Megamarket Conformance Plan

Task-064 suite v1 is mandatory before provider admission. The candidate uses only synthetic merchant tokens and the Task-029 Linux sandbox fixture. Required machine evidence is committed as `docs/connectors/megamarket/conformance-report.json`.

Provider-specific tests cover manifest read-only boundaries, strict merchant/scheme/warehouse configuration, X-Merchant-Token isolation, product `searchAfter` pagination, stock-by-offer warehouse normalization, bounded order offset pagination, buyer-PII exclusion, malformed/duplicate/negative response rejection, normalized rate/service errors and raw secret/error non-disclosure.
