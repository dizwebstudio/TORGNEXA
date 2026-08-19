# Task 091: Sms Provider Sdk

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- SMS provider-neutral service with marketing consent, quotas, phone fingerprinting, callback dedupe and migration 000054.
- Architecture review `ARCH-091` and executable tests are included.

## Objective
Add provider-neutral SMSProvider to Notification Center with transactional-vs-marketing policy, delivery status, quotas and privacy controls.

## Dependencies
022, 060, 063, 076

## Deliverables
Provider port/capabilities, API/config, message/delivery normalization, rate/tenant quotas, redaction/retention, webhook idempotency and reference fake provider.

## Acceptance
Phone numbers are treated as PII; marketing path cannot bypass consent policy; delivery callbacks dedupe; failures participate in notification fallback safely.

Run required repository checks and report results, risks and follow-ups.