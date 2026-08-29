# Conformance plan

Run the mandatory Connector SDK v1 conformance suite with synthetic webhook
credentials only. The candidate covers authentication boundaries, normalized
errors, retry/rate-limit semantics, idempotency/replay, tenant isolation,
resource limits and production-credential rejection.

The current report records all 13 of 13 checks passing, including
`sandbox_isolation`. The suite was run with the static connector emulator and
Linux user namespaces enabled; the resulting report is the release evidence
for this connector.
