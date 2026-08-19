# RUTUBE Connector

Task `046` implements the RUTUBE video-platform adapter behind Connector SDK v1.

The repository deliberately does **not** hard-code Studio/private HTTP endpoints. As of the 2026-08-12 audit, RUTUBE publishes current player/embed API documentation, but a current open upload API contract was not publicly available; the old official `rutube/php-api-client` is explicitly unmaintained. Production upload therefore uses a typed `PartnerTransport` that must be bound to the official account-specific RUTUBE partner contract obtained for the tenant/channel.

Declared capability: `social.post.video` only. TORGNEXA owns scheduling and publication orchestration; comments/analytics/edit/delete are not declared until an official current contract is qualified.

See `spec.md`, `capability-audit.md`, `reconciliation.md`, and `conformance-plan.md`.
