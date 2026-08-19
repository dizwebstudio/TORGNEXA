# Production qualification evidence

`make production-qualification` is the P2 deployment gate. It fails closed when Docker is unavailable and records evidence for the exact topology it exercised.

The gate performs:

1. the deterministic Task-066 SLO regression;
2. a fresh isolated Compose deployment;
3. Outbox -> Kafka -> Inbox end-to-end delivery using the application image;
4. duplicate immutable-event delivery followed by a marker event to prove Inbox idempotency and continued consumer progress;
5. a durable `LOST` warehouse incident with an explicit backup route, positive backup ATP, append-only routed evidence, and proof that source physical stock is unchanged;
6. black-box API load against the Task-066 API availability/p99/throughput limits;
7. graceful worker restart;
8. Kafka restart/recovery;
9. PostgreSQL restart/recovery;
10. repeat runtime probes, including warehouse incident automation, after every failure drill.

Generated evidence is written below `qualification/evidence/<UTC timestamp>/` and is intentionally not committed. Production release evidence must be retained by the release system together with image digests and infrastructure metadata.

## P4 go-live qualification

`make p4-qualification` is the final go-live evidence synthesizer. Unlike P2/P3 repository/topology gates, it intentionally requires external facts and therefore normally runs from a protected release/change runner, not a developer workstation.

Required inputs:

- exact clean Git tag `v$TORGNEXA_P4_VERSION` and Go 1.26.5;
- Docker Compose v2 for the full P3 topology/restart/restore/upgrade drills;
- `TORGNEXA_P4_REPOSITORY=OWNER/NAME` and `TORGNEXA_P4_PROTECTED_BRANCH`;
- optional `TORGNEXA_P4_GITHUB_TOKEN` for private GitHub repositories; it is never retained;
- HTTPS `TORGNEXA_P4_BASE_URL` plus environment-only `TORGNEXA_P4_BEARER_TOKEN`;
- absolute `TORGNEXA_P4_CONNECTOR_PLAN`, based on `qualification/live-connectors.example.json`;
- absolute `TORGNEXA_P4_POSTURE_FILE`, based on `qualification/production-posture.example.json`;
- absolute downloaded/unpacked `TORGNEXA_P4_RELEASE_EVIDENCE_DIR` from the exact protected release workflow;
- environment-only `TORGNEXA_P4_GITHUB_RELEASE_TOKEN` with Contents read access during qualification so P4 can bind local evidence to staged draft asset digests; promotion requires the same variable to hold a token permitted to update the draft release;
- absolute `TORGNEXA_P4_SECURITY_TOOLS_DIR` containing the checksum-verified `cosign` used for independent verification.

The GitHub branch-rules capture must prove an active ruleset workflow pinned by SHA to `.github/workflows/architecture-required.yml`, deletion/force-push protection, pull-request approvals and a required Team reviewer for architecture paths. It is not possible to replace those hosted facts with a local `PASS` flag.

Live connector plans contain account/connector IDs only. Every connector account that is `active` in the target tenant must be listed; omission fails P4. Each listed account receives two consecutive remote health checks. `run_sync` is off by default; when deliberately enabled, the caller must additionally set `TORGNEXA_P4_ALLOW_REMOTE_SYNC=I_UNDERSTAND_THIS_MAY_WRITE` because the account's active sync policy may write provider state.

A successful run writes `p4-go-live.json` plus digested subordinate evidence below `qualification/evidence/p4-<UTC>/`. This directory is not source material and must remain ignored by Git and Docker build contexts.

After a retained P4 PASS, public publication is a separate explicit operation: `TORGNEXA_P4_GO_LIVE_EVIDENCE=/abs/path/p4-go-live.json TORGNEXA_P4_GITHUB_RELEASE_TOKEN=... make p4-publish`. The promoter re-verifies the P4 root and every subordinate hash, requires the exact clean release tag, proves that the draft still has exactly the verified asset set with unchanged digest/size, uploads `p4-go-live.json` as the final audit asset, and only then clears the draft flag.
