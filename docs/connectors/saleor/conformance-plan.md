# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic credentials,
tenant-isolation probes, retry/error normalization, idempotency/replay and Linux
sandbox isolation. Production credentials are forbidden in the harness.

The canonical report is **13/13 PASS** (`conformance-report.json`), including
the shared `sandbox_isolation` probe. This report certifies the Connector SDK
boundary only; it does not certify a particular Saleor installation.

The separate credentialed store gate is documented in
`docker-live-qualification.md`: the disposable official Saleor Platform Docker
stack passed read, product/price/stock write, read-after-write and cleanup smoke
on 2026-08-29. External merchant staging remains a separate gate and is still
blocked until an operator supplies a non-production endpoint and App token.
