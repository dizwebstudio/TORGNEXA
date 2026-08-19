# Task 118: P4 Go-Live Production Readiness

## Status
`done` — repository implementation complete on 2026-08-18.

`deployment_qualification_status: pending_external_execution` until `make p4-qualification` passes for one exact tagged release using its real production topology, hosting policy, signed public release evidence, and live connector accounts.

## Objective
Close the P4 repository gap between P3 code/runtime qualification and an auditable go-live decision. P4 must never manufacture hosted, cryptographic, infrastructure, secret-management or seller-account facts from source code.

## Scope
- add a single fail-closed `make p4-qualification` entry point that requires Go 1.26.5, Docker Compose v2, a clean exact release tag, P3 topology evidence, GitHub hosted branch-rule evidence, reviewed production posture, independently verified public release evidence and live connector checks;
- query the GitHub applied-rules API for the exact protected branch and require deletion protection, force-push protection, PR approvals, a Team reviewer for architecture paths and a pinned ruleset workflow;
- add `.github/workflows/architecture-required.yml` as the canonical immutable ruleset workflow source using the trusted-base architecture verifier;
- independently reverify downloaded Sigstore signatures and GitHub SLSA provenance against exact repository/ref/SHA/workflow/trigger identity;
- qualify every active live connector account through the public TORGNEXA API and fail when any active account is omitted from the qualification plan, retain no bearer credential, require two consecutive healthy remote checks, and make any remote sync an explicit operator opt-in;
- validate a non-secret production posture statement covering secret backend, rotation/break-glass, TLS, encrypted restore, retention, on-call, rollback and incident/alert rehearsal;
- replace the release publication placeholder with fail-closed staging of a non-public GitHub Release draft; expose promotion only after retained P4 PASS evidence;
- retain all P4 evidence under an ignored/non-build-context qualification directory.

## Safety invariants
- P4 PASS is impossible without real Docker, Go 1.26.5, a clean exact tag and all external evidence inputs;
- API/GitHub bearer tokens are environment-only and never written to retained evidence;
- retained P4 JSON rejects secret-shaped fields;
- live connector qualification never uploads or rewrites credentials, and every active account must be explicitly covered by the non-secret plan;
- remote sync is disabled unless both the connector plan requests it and `TORGNEXA_P4_ALLOW_REMOTE_SYNC=I_UNDERSTAND_THIS_MAY_WRITE` is present;
- ordinary repository status checks do not satisfy ARCH-OPS-01: the applied rules must include the GitHub `workflows` rule pinned to `.github/workflows/architecture-required.yml` and a required Team reviewer for architecture paths;
- public release evidence is cryptographically reverified outside the signing job before it can contribute to P4 PASS;
- release staging is tag-push-only, exact-context-bound and draft-first; partial upload never becomes public; promotion requires a locally reverified retained `p4-go-live.json` PASS root, an unchanged exact staged asset set, and publishes that root report as the final audit asset;
- a `workflow_dispatch` release rehearsal must be invoked from the exact `vVERSION` tag so OIDC/ref identity cannot diverge from evidence metadata.

## Deliverables
- `scripts/check-p4-go-live.sh` / `make p4-qualification`;
- `scripts/p4_common.py`, `p4_live_connectors.py`, `p4_hosting_rules.py`, `p4_posture.py` and policy tests;
- `scripts/verify-release-evidence-external.sh`;
- `scripts/package-release-evidence.sh`, `stage-github-release.sh`, `p4_release_stage.py`, `p4_root_evidence.py` and `promote-github-release.sh`;
- `.github/workflows/architecture-required.yml`;
- production posture and live connector plan examples;
- release workflow draft-first publisher integration;
- ADR-0093 and ARCH-118 review.

## Acceptance
- P4 Python policy tests and source AST/syntax checks pass;
- all new shell scripts pass `bash -n`;
- architecture/supply-chain policy accepts the new immutable required workflow and reviewed publisher;
- P3 migration/API contracts remain unchanged at 74 migrations and 107 OpenAPI operations;
- a real P4 run emits `p4-go-live.json` with `status: PASS` only after P3, hosting, posture, independent release and connector evidence pass;
- missing Docker, wrong Go toolchain, dirty/un-tagged source, absent live token, missing hosted workflow rule, failed Sigstore verification, unhealthy connector or insecure production posture is a hard failure;
- repository completion does not claim `deployment_qualified` until the external command has actually passed and its evidence has been retained.
