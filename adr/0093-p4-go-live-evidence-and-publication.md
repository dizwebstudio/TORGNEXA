# ADR-0093: P4 go-live evidence synthesis and draft-first publication

Status: Accepted

## Context

P3 closes transactional commerce execution and gives TORGNEXA reproducible topology qualification, but source code cannot prove the final hosted and operational facts required for a go-live decision. GitHub rulesets, OIDC signatures, current image scans, production secret handling, real backup topology and seller credentials exist outside the repository. Treating repository completion as proof of those facts would turn a safety gate into documentation theater.

The release workflow also retained an obsolete publication placeholder after the repository license was approved. Signed and verified release bytes could never be published, so the repository did not yet provide a controlled public release transport.

## Decision

P4 adds one fail-closed root command, `make p4-qualification`, bound to a clean exact semantic-version tag, Go 1.26.5 and Docker Compose. It executes P3 qualification and then verifies four external evidence classes: GitHub rules actually applied to the protected branch, recent reviewed non-secret production posture, independently reverified release signatures/provenance, and live connector health through the public TORGNEXA API.

Hosted architecture protection is accepted only when applied GitHub rules expose deletion/force-push protection, pull-request approvals, a Team reviewer for architecture paths, and a SHA-pinned required workflow at `.github/workflows/architecture-required.yml`. Release identity is accepted only when Sigstore and SLSA evidence bind each first-party binary to the exact repository, release workflow, tag ref, source commit and push trigger.

Publication is split into staging and promotion. The protected release workflow may create only a **draft** GitHub Release after verification and upload the manifest, deterministic evidence bundle and verified binaries. P4 independently compares staged GitHub asset SHA-256 digests with the verified local bytes. `make p4-publish` may remove the draft flag only when given retained `p4-go-live.json` evidence with `status: PASS`.

Live connector evidence is deliberately non-secret: it records account identifiers, connector identifiers, health categories and timestamps. Two consecutive remote health checks are required. Optional sync qualification is guarded by a separate acknowledgement because a configured sync policy may write remotely.

## Migration and data impact

P4 adds no database migration and changes no commerce state schema. The migration catalog remains at 000074. Qualification evidence is operational output and is excluded from Git and Docker build contexts.

## Compatibility impact

The public HTTP API and generated SDK remain unchanged at the P3 surface. Release workflow behavior is tightened: manual `workflow_dispatch` must select the exact `vVERSION` tag; branch-based release rehearsal is rejected because it cannot share the tag-bound OIDC identity encoded in release evidence.

## Security and privacy impact

P4 rejects secret-shaped fields in retained JSON and never serializes bearer/GitHub credentials. Release staging uses the minimum `contents: write` permission inside the protected `release-publication` environment after signature/provenance verification; final promotion is a separate explicit P4 action bound to PASS evidence. GitHub hosted rules and live provider health are measured rather than self-attested.

Production posture evidence is intentionally non-secret. It proves reviewed control state, not secret values. Secret rotation and break-glass exercises remain operator-controlled external facts.

## Operational impact

A go-live claim now has one machine-readable root, `p4-go-live.json`, whose digests bind the P3 topology result, hosted protection result, production posture result, independent release verification result and live connector result. Operators can retain the directory with the release ticket/change record.

The command fails closed when Docker, the pinned Go version, exact release tag, GitHub rules, Sigstore identity, connector health or posture evidence is unavailable. This is expected behavior on a development workstation.

## Consequences

Repository P4 completion means TORGNEXA contains the complete qualification and publication machinery. It does not mean an arbitrary deployment is production-qualified. Only an actual retained PASS for one exact release/version/topology may be used as a go-live claim.

## Alternatives considered

A checklist-only P4 was rejected because operators could mark external controls complete without machine evidence. Treating ordinary required status checks as an immutable architecture gate was rejected because a pull request can change repository-local workflow behavior unless a ruleset workflow is sourced/pinned independently. Publishing directly without a draft was rejected because an upload failure could expose a partial release. Storing live connector credentials in a qualification plan was rejected because qualification evidence is long-lived and routinely archived.
