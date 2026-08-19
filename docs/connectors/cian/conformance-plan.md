# CIAN conformance plan

Run the Task-064 thirteen-check provider harness with the CIAN candidate through the Linux connector sandbox emulator. Required evidence covers manifest/SDK compatibility, Task-021 secret boundary, normalized health/errors, rate-limit policy, generic idempotency/webhook fixture compatibility, tenant isolation, dry-run suppression, production-secret rejection, egress grant, resource ceiling and sandbox isolation.

Provider-specific tests additionally cover:
- manifest remains status-only and rejects API publication-write capability;
- exact configured feed-URL health binding;
- exact remote import/order-ID status binding;
- successful/problem import-report projection;
- 429 retry normalization;
- deterministic CIAN Feed v2 apartment-sale XML;
- rental `LeaseTermType` versus sale `SaleType` separation;
- description/area/phone/photo/category constraints and duplicate `ExternalId` rejection.
