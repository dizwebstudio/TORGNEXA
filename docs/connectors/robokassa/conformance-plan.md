# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic credentials, tenant-isolation probes, retry/error normalization, idempotency/replay and Linux sandbox isolation. Production credentials are forbidden in the harness.

12 of 13 checks pass in the current execution environment; `sandbox_isolation` cannot be exercised here because this environment cannot create unprivileged Linux user namespaces (`unshare --user` returns `Operation not permitted`), which the Task-029 sandbox probe requires regardless of connector. This is an environment constraint, not a Robokassa-specific defect — the same probe exercises the shared emulator, not any Robokassa code path. Re-run in an environment with user namespaces enabled to obtain a full pass.
