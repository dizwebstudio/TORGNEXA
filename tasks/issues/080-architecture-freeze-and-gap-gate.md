# Task 080: Architecture freeze and gap gate

## Objective

Add a fail-closed repository and pull-request architecture gate. Core pillar
changes require a new accepted ADR and complete impact analysis; new domains
and providers require a task-linked gap audit; provider work cannot bypass the
Connector SDK or enter generic Core/App/Platform packages.

## Dependencies

024, 067

## Deliverables

- canonical frozen-pillar, module, provider, and sensitive-path policy;
- append-only structured architecture review contract and Task 080 record;
- bounded offline current-tree and exact merge-base-to-head checkers;
- full first-party Go inventory, dependency-direction and provider-neutrality
  enforcement;
- provider admission, evidence, SDK-route, retirement, and conformance gates;
- trusted-base pull-request verifier plus fail-closed workflow policy;
- ADR/gap-audit/PR templates, governance documentation, and adversarial tests.

No REST, event, protobuf, webhook, database, or runtime capability contract is
changed by this task.

## Acceptance criteria

- `ARCH-POL-01`: policy and review JSON are strict, bounded, schema-valid,
  canonical, sorted where required, symlink-safe, and produce deterministic
  sanitized diagnostics.
- `ARCH-SRC-01`: every first-party Go file is inventoried from an explicit
  fail-closed root; unknown roots, all root/nested vendor trees, unregistered
  packages, cgo/linkname escapes, forbidden dependency directions, and
  provider-specific generic code fail.
- `ARCH-ADR-01`: frozen-pillar, gate, and mixed changes require a newly added
  accepted ADR with meaningful compatibility, migration/data,
  security/privacy, operational, alternatives, decision, context, and
  consequence sections. Accepted or cited ADRs are immutable.
- `ARCH-DIFF-01`: add/modify/delete/rename/copy changes are bound to a newly
  added review of the correct class and exact scope. New domains cannot use an
  implementation review; mixed provider/Core work uses one matching mixed
  record covering both sides.
- `ARCH-GIT-01`: diff mode requires exact lowercase commit IDs, full clean
  non-sparse history, exact base ancestry/current merge base, materialized
  tracked files, no ignored or ordinary untracked files, and bounded
  NUL-delimited Git output.
- `ARCH-PROV-01`: provider admission is closed until Tasks 010, 025, 029, and
  064 were already completed in the merge base. Each completion transition is
  bound to its first fresh task review and exact issue scope; completed
  prerequisite issues are immutable. Each admitted provider has a canonical
  registered root, manifest/spec/capability/conformance evidence, a buildable
  Connector SDK import, and no direct Core/App/database access.
- `ARCH-RET-01`: provider removal creates an explicit reviewed retirement
  tombstone tied to the prior merge-base registration. A new unregistered
  connector/plugin, non-Go executable, hidden payload, or synthetic tombstone
  fails closed.
- `ARCH-CI-01`: the repository PR workflow builds the verifier offline from the
  exact detached base before running HEAD code; workflow policy rejects
  shallow/sparse checkout, mutable actions, job skips, secrets, deployment
  environments, additional privilege-bearing jobs, and wrapper substitution.
- `ARCH-TEST-01`: deterministic unit, race, repeated, synthetic-Git, contract,
  supply-chain, shell, and main repository checks cover the success path and
  the listed bypass classes.
- `ARCH-OPS-01`: a protected post-merge pull request proves that a hosting
  Ruleset Required Workflow (or equivalent immutable external check), required
  architecture reviewer, and branch policy force the trusted-base verifier to
  run. An ordinary required status by mutable job name does not satisfy this
  criterion.

## Status

Repository-local implementation completed on 2026-08-09 and extended by Task 118 with a canonical `.github/workflows/architecture-required.yml` plus an applied-rules API verifier. Overall operational acceptance remains blocked until the real protected branch exposes the required workflow, Team architecture reviewer, deletion/force-push protection and PR policy through GitHub applied-rules evidence.

```yaml
local_implementation_status: completed
operational_architecture_gate_status: blocked
required_workflow_qualification: pending
```

The first protected post-merge pull request whose base already contains this checker must prove that the active ruleset pins `.github/workflows/architecture-required.yml` by SHA and requires a Team reviewer for architecture paths. Task 118 captures those active rules through GitHub REST; an ordinary required status with the same mutable job name remains unacceptable evidence.

Run required repository checks and report results, risks, external
qualification, and follow-ups.
