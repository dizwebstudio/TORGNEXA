# Architecture v1.0 Freeze

Core is frozen around generic capabilities. A normal implementation within an
existing decision records explicit impact; a new domain/provider performs the
full gap audit; a changed frozen pillar, architecture gate, or mixed
provider/Core change additionally adds a new or superseding ADR.

## Canonical pillars

The IDs below are immutable policy identifiers mirrored exactly in
`architecture/policy.json`:

1. `universal-commerce-content-fulfillment-models` — universal commerce,
   content, and fulfillment models;
2. `pim-mdm-financial-stock-ledgers` — PIM/MDM and financial/stock ledgers;
3. `connector-plugin-runtime-capabilities` — Connector/Plugin Runtime and
   capability model;
4. `event-platform-outbox-inbox` — durable event platform with outbox/inbox;
5. `bidirectional-sync-reconciliation` — sync and reconciliation;
6. `workflow-approval-audit-lineage` — workflow, approvals, audit, lineage;
7. `privacy-secrets-security-governance` — privacy, secrets, security,
   governance;
8. `reporting-settlements-growth-supply-planning` — reporting, settlements,
   growth, and supply planning;
9. `developer-surfaces` — REST, webhooks, n8n, MCP, OpenClaw surfaces;
10. `russia-compliance-generic-ports` — Russian compliance behind generic
    ports;
11. `legal-party-product-compliance-fx` — canonical legal party, product
    compliance, and sourced FX;
12. `enterprise-iam-siem-cloud-upload-edge` — Enterprise IAM, SIEM, Cloud
    billing, upload security, and security edge.

Tasks 081–092 describe remaining post-audit backlog coverage. Their presence in
the backlog is not implementation evidence and does not change the freeze.

## Required evidence by change class

| Change | Structured review | Full gap audit | New/superseding ADR | Provider evidence |
|---|---:|---:|---:|---:|
| Existing Core/Platform/process implementation | yes | yes | only if pillar changes | no |
| New domain/module | yes | yes | when it changes a pillar | no |
| New provider | yes | yes | only if generic architecture changes | yes |
| Frozen pillar or gate | yes | yes | yes | no |
| Provider plus Core/SDK | yes (`mixed`) | yes | yes | yes |

Every impact status needs a meaningful rationale. A blank value, `N/A`,
`TODO`, or `TBD` is not evidence. Public contracts still use Task 024 and SQL
still uses Task 067; this gate records impact and does not replace either one.

## Enforcement

- `make architecture` and `scripts/check-architecture.sh` validate the current
  tree offline: strict records/references, a bounded full-repository Go-source
  inventory with fail-closed roots, path and symlink safety, module direction,
  forbidden root or nested `vendor` trees, and provider-specific code outside
  connector/plugin roots. The checker TCB
  package contains provider-pattern fixtures by necessity; its whole directory
  is self-protected and is evaluated by the trusted-base verifier.
- The repository pull-request workflow checks out full history, builds
  `architecturecheck` offline from the exact trusted base revision in a
  detached worktree, and executes that binary before any code from the
  pull-request HEAD. It rejects shallow/sparse, dirty, mismatched, stale, or
  even ignored-untracked checkouts. The checker evaluates the
  merge-base-to-head NUL-delimited diff and treats add, modify, delete, rename,
  and copy as relevant.
- A protected path must be covered by the newly added record of the class that
  triggered it. Provider code and mutable provider evidence additionally bind
  to the same `provider.id`; mixed work must use one mixed record that covers
  both provider and Core/Platform paths. Decision ADRs must already be
  `Accepted`, contain meaningful mandatory sections, be newly added, and every
  accepted or cited ADR is immutable thereafter.
- Ordinary architecture evidence remains `NNN-kebab-case.json` / `ARCH-NNN`.
  Canonically split tasks may add an independent append-only supplemental
  `NNN[a|b]-kebab-case.json` record with `stage: a|b` and matching
  `ARCH-NNNA/B` ID. A staged review is fresh evidence only in the change that
  adds it and does not itself mark the numbered parent task complete. This
  preserves exact changed-path review for later mandatory stages such as
  `089b` without mutating earlier accepted evidence.
- Tasks 010, 025, 029, and 064 are repository-complete. Provider admission remained disabled in the Task-064 completion change. Task 011 is the later reviewed admission decision and registers the first read-only provider; the protected trusted-base workflow still requires all four prerequisites to be recognized as complete in the merge base before that admission change qualifies.
  Enabling it is itself a protected pillar decision, and completion of all four
  prerequisites must already be present in the merge base rather than claimed
  in the admission pull request. Each incomplete-to-completed transition for
  those task issues must be in the same change as its first task-bound
  architecture review with exact issue scope. Once completed in the merge
  base, the prerequisite issue is immutable; rename, deletion, or rewritten
  acceptance evidence fails.
- A registered provider can be retired only by the same reviewed change that
  removes its merge-base registration and implementation sources. The policy
  keeps an immutable explicit retirement record, the implementation directory
  retains only a non-executable `manifest.json`, and canonical documentation
  evidence remains available. New unregistered connector/plugin directories,
  invented retirement records, or residual code/WASM/JS/binary payloads fail.
- The PR checklist helps humans but is not accepted as machine evidence.

The gate cannot prove that prose is truthful, recognize every obfuscated
provider reference in non-Go data, or prove connector behavior from an import;
human review and the Task 064 machine-readable conformance suite remain mandatory. Repository
files also cannot prove that the hosting platform will execute this workflow:
a pull request may modify its own workflow, and a required status identified
only by job name is not an immutable trust boundary. A repository or
organization Ruleset Required Workflow (or equivalent external immutable
check), branch policy, and architecture reviewer must be configured outside the
repository. The first post-merge protected hosted PR whose base already
contains this checker must retain evidence that those controls forced the
trusted-base verifier to run; the introduction PR cannot qualify that external
control.
