# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic credentials, tenant-isolation probes, retry/error normalization, idempotency/replay and Linux sandbox isolation. Production credentials are forbidden in the harness. Add a route-level webhook check using a synthetic callback and a deterministic status re-fetch; live acquiring-bank delivery is a release-topology gate, not a repository test.
