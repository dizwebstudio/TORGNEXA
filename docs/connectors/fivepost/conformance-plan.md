# Conformance plan

Run the mandatory Task-064 Connector SDK suite with synthetic credentials,
tenant-isolation probes, normalized error/retry checks, idempotency and Linux
sandbox isolation. The candidate transport is deterministic and never calls
5Post. Production credentials and recipient data are forbidden in the
conformance harness.
