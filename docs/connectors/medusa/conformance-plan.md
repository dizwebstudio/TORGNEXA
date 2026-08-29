# Conformance plan

The canonical Task-064 report is **PASS 13/13** with synthetic credentials,
tenant-isolation probes, retry/error normalization, idempotency/replay and the
Linux sandbox check. Production credentials are forbidden in this harness; the
report is [conformance-report.json](conformance-report.json).

SDK conformance is not proof that a particular Medusa installation, API
version, ACL or key works. The separate credentialed gate is documented in
[docker-live-qualification.md](docker-live-qualification.md) and executed by
`scripts/medusa-smoke.sh`. The repository Docker DTC Starter smoke passed on
2026-08-29 (read and write reconciliation with automatic restoration). Medusa
remains repository-qualified, not externally live-qualified, until credentials
for a separate staging endpoint are provided and that endpoint passes the same
smoke.
