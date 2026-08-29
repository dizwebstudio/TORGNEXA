# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic credentials, tenant-isolation probes, retry/error normalization, idempotency/replay and Linux sandbox isolation. Production credentials are forbidden in the harness.

All 13 of 13 checks pass in the current execution environment; the canonical result is recorded in [conformance-report.json](conformance-report.json). This SDK conformance result is separate from the credentialed Docker/live qualification in [docker-live-qualification.md](docker-live-qualification.md): a passing candidate does not imply that an arbitrary merchant endpoint or Integration credential is configured.
