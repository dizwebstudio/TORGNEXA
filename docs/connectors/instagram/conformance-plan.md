# Instagram Connector Conformance Plan

Run the Task-064 thirteen-check provider harness with the Instagram candidate through the Linux connector sandbox emulator. Required evidence covers manifest/SDK compatibility, Task-021 secret boundary, normalized health/errors, rate-limit policy, generic idempotency/webhook fixture compatibility, tenant isolation, dry-run suppression, production-secret rejection, egress grants, resource ceiling and sandbox isolation.

Provider-specific tests additionally cover:
- exact professional-account health binding;
- manifest denies text-only publishing;
- Task-088 media re-open before staging;
- JPEG/MP4 size/type checks and caption/carousel limits;
- safe short-lived HTTPS staging URL validation;
- container `FINISHED` flow and remote ID binding;
- non-retryable ambiguous write normalization.
