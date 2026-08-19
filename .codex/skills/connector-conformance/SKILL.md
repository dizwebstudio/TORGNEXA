# Connector Conformance

Use Task-064 `internal/platform/connectors/conformance` for every official/community provider before admission.

1. Implement a provider-local `conformance.Candidate` adapter using only the approved Connector SDK prefix.
2. Run all thirteen required checks; required checks have no skip state.
3. Persist the validated machine report at `docs/connectors/<connector-id>/conformance-report.json`.
4. Treat any auth/health/error/retry/idempotency/webhook/tenant/dry-run/credential/egress/resource/isolation failure as release-blocking.
5. Never copy raw provider errors, response bodies, credentials, Authorization headers or PII into conformance evidence.
6. Keep provider admission fail-closed until the protected architecture gate accepts the provider and its passing report.

Repository self-qualification: `make conformance`.
