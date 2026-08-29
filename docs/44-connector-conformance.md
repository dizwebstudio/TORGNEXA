# Connector Conformance Suite

Task 064 defines the mandatory Connector SDK v1 certification boundary for every official, verified, community, or private provider before architecture admission.

The reusable Go runner lives at:

`internal/platform/connectors/conformance`

Provider packages may import this subpackage because it remains inside the approved `internal/platform/connectors` SDK prefix. The harness does not grant Core, database, concrete-secret, host-network, filesystem, or process authority.

## Required suite v1

A report passes only when all thirteen checks pass; required checks cannot be skipped:

1. `manifest_sdk` — manifest validates against Connector SDK major 1 and the Task-025 admission identity/capability grant matches the manifest.
2. `auth_boundary` — valid sandbox/test auth works and rejected auth is returned as a normalized SDK error rather than raw provider text.
3. `health_normalization` — account/manifest binding and provider health return only the bounded SDK health model.
4. `normalized_errors` — provider failure is represented by `RemoteError` category/code/request-id/retry metadata only.
5. `rate_limit_retry` — retryable rate limits produce bounded deterministic host scheduling and stop at the manifest attempt ceiling.
6. `idempotency` — repeating the same idempotency key does not apply the business effect twice and returns the same effect fingerprint.
7. `webhook_replay` — repeating one delivery identifier is recognized as a duplicate without applying the webhook effect twice.
8. `tenant_isolation` — a tenant cannot read another tenant's provider resource while its own scope remains reachable.
9. `dry_run_side_effect_suppression` — Task-029 dry-run performs no secret-provider or network-transport call.
10. `production_credential_rejection` — Task-029 test mode rejects production-tier credentials before secret-broker access.
11. `egress_grant_enforcement` — ungranted destinations fail closed; granted dry-run destinations become mediated external-action intents only.
12. `resource_limit_failure` — an output-budget violation is surfaced as the normalized Task-029 resource-limit outcome.
13. `sandbox_isolation` — the Linux reference probe confirms environment, filesystem, direct network, production credentials, mediated egress, and resource-limit enforcement.

## Provider adapter

A provider supplies a small conformance adapter implementing `conformance.Candidate`. The adapter exposes observable behavior only: Connector SDK root object, tenant fixture/account/runtime, deterministic behavioral probes, and a Task-029 sandbox fixture. It is not an escape hatch into provider internals.

A provider conformance test is expected to call:

```go
report := conformance.Run(ctx, candidate, tenantA, tenantB, time.Now)
if err := conformance.Require(report); err != nil {
    t.Fatal(err)
}
```

The provider release pipeline then writes the validated report to the canonical evidence path:

`docs/connectors/<connector-id>/conformance-report.json`

After Task 064 is in the protected branch merge base, the architecture checker requires every admitted provider to have a valid passing report at this path. The report `connector_id` must equal the policy provider id.

### SDK conformance is not live qualification

The thirteen checks run against deterministic candidates and the Task-029
sandbox. They prove the Connector SDK boundary, not a provider's current
installation, API version, credentials, rate limits or data semantics. A
provider may therefore have a passing `conformance-report.json` while its
credentialed Docker/live qualification is still blocked. For CS-Cart the
executable gate is `scripts/cscart-smoke.sh`, documented in
`docs/connectors/cs-cart/docker-live-qualification.md`. Magento / Adobe
Commerce uses the same split: `scripts/magento-smoke.sh` is documented in
`docs/connectors/magento/docker-live-qualification.md`; its canonical SDK
report does not imply that a merchant store or Integration token works.

## Machine-readable report

The contract is:

`contracts/plugins/conformance-report-v1.schema.json`

The report contains only connector identity, SDK/suite versions, ordered pass/fail checks, normalized reason codes, UTC completion time, and a SHA-256 digest over the report with the digest field cleared. Raw provider errors, response bodies, credentials, Authorization headers, or PII are not report fields.

`conformance.WriteJSON` refuses to serialize a structurally invalid report, and `conformance.Require` fails unless all required checks pass.

## Reference qualification

`make conformance` builds the deterministic Task-029 emulator and `cmd/torgnexa-connector-conformance`, then executes all thirteen checks. On Linux it also runs the external namespace/chroot isolation probe. The script injects a synthetic host production secret and verifies that it never appears in the machine report.

Task 064 adds no provider implementation and does not open provider admission in the same change. Task 080 deliberately requires Tasks 010, 025, 029, and 064 to already be completed in the merge base before `provider_admission.enabled` can become true. Therefore admission is opened only by a later protected architecture change after this task lands.
