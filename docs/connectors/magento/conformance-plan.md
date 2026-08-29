# Conformance plan

The canonical Task-064 report is **PASS 13/13** with synthetic credentials,
tenant-isolation probes, retry/error normalization, idempotency/replay and the
Linux sandbox check. Production credentials are forbidden in this harness; the
report is [conformance-report.json](conformance-report.json).

SDK conformance is not proof that a particular Magento installation, API
version, ACL or credential works. The separate credentialed gate is documented
in [docker-live-qualification.md](docker-live-qualification.md) and executed
by `scripts/magento-smoke.sh`. Until that smoke passes on a non-production
store, Magento remains repository-qualified, not live-qualified.
