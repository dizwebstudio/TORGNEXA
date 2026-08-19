# Auto.ru conformance plan

Run the Task-064 thirteen-check provider harness with the Auto.ru candidate under the Linux connector sandbox emulator. Required evidence covers manifest/SDK compatibility, secret boundary, normalized health/errors, rate-limit/retry policy, host idempotency fixture, webhook-replay fixture compatibility, tenant isolation, dry-run suppression, production-secret rejection, egress grant, resource ceiling and sandbox isolation.

Provider-specific tests additionally cover:
- exact Auto.ru account binding and string/numeric account ID handling;
- current dealer-offer response mapping (`car_info`, document year and pagination counters);
- NEW/USED vehicle XML validation and deterministic encoding;
- feed submit/status mapping;
- fail-closed ambiguous write behavior with no automatic retry;
- read/write classified risk registration.
