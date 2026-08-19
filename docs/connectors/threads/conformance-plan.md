# Threads Connector Conformance Plan

Run the Task-064 thirteen-check provider harness with the Threads candidate through the Linux connector sandbox emulator. Required evidence covers manifest/SDK compatibility, Task-021 secret boundary, normalized health/errors, rate-limit policy, generic idempotency/webhook fixture compatibility, tenant isolation, dry-run suppression, production-secret rejection, egress grants, resource ceiling and sandbox isolation.

Provider-specific tests additionally cover:
- exact Threads user health binding;
- text/image/video capability validation;
- 500-code-point text limit;
- Task-088 image staging path;
- safe public HTTPS staging URL boundary;
- container/publish flow and foreign remote-ID rejection;
- short-to-long token exchange using `th_exchange_token`;
- long-lived refresh using `th_refresh_token`;
- host-owned secret rotation with no plaintext token in result;
- non-retryable ambiguous write normalization.
