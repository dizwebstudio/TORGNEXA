# Conformance plan

Run the mandatory Connector SDK v1 conformance suite with synthetic webhook
credentials only. The candidate covers authentication boundaries, normalized
errors, retry/rate-limit semantics, idempotency/replay, tenant isolation,
resource limits and production-credential rejection.

The current report records 12 of 13 checks passing. `sandbox_isolation` is
environment-gated: this execution environment cannot create unprivileged
Linux user namespaces (`unshare --user` returns `Operation not permitted`).
That shared harness limitation is not a Bitrix connector failure; rerun the
suite on a host with user namespaces enabled before a release qualification.

