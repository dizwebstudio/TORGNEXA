# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic
credentials, tenant-isolation probes, retry/error normalization,
idempotency/replay and Linux sandbox isolation. Production credentials are
forbidden in the harness.

The canonical report is **13/13 PASS** (`conformance-report.json`), including
the shared `sandbox_isolation` check. This certifies the Connector SDK boundary,
not a particular Shopify store.

Shopify's separate credentialed gate is documented in
[docker-live-qualification.md](docker-live-qualification.md). The Docker
protocol double passed on 2026-08-29 for Admin REST API `2026-07`, including
read/write reconciliation and cleanup. A real Shopify Dev Store, app token and
synthetic SKU are still required for external live qualification.
