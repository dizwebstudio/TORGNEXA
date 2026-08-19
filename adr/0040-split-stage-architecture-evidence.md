# ADR 0040: Supplemental architecture evidence for split implementation stages

Status: Accepted

## Context

The canonical execution plan intentionally splits Tasks 076, 088, and 089 into `a` and `b` stages. Architecture reviews are append-only and historically allowed exactly one `ARCH-NNN` record per numbered task. That model works for a single protected change but prevents a later mandatory stage from supplying fresh evidence for newly changed sensitive paths. Task 076b had to avoid sensitive runtime changes for this reason; Task 089b cannot do so because it must add FX storage and adapters after Tasks 049, 058, and 059.

## Decision

Keep the existing `NNN-kebab-case.json` / `ARCH-NNN` review format unchanged for ordinary tasks and first-stage evidence. Add an optional supplemental stage form `NNN[a|b]-kebab-case.json` whose record uses the same numbered `task`, an explicit `stage` value (`a` or `b`), and ID `ARCH-NNNA` or `ARCH-NNNB`. Supplemental records are independently append-only, must satisfy the same impact/ADR/provider rules, and are considered fresh evidence only in the diff that adds them. A staged record does not make the parent task complete; task completion continues to come from the canonical issue status and execution plan.

Task 089a continues to use the legacy-compatible `ARCH-089` record so the trusted Task-064 checker can validate this change. The new staged form exists for the later `089b` protected change and other explicitly decomposed tasks.

## Consequences

Split tasks can make multiple independently reviewed protected changes without mutating previous evidence or weakening exact changed-path binding. Existing review files remain valid byte-for-byte. Review IDs remain globally unique because stage suffixes are part of the ID. Tooling and the governance JSON Schema gain optional stage syntax, but ordinary task authors see no format change.

## Alternatives considered

Mutating `ARCH-089` during Task 089b was rejected because architecture evidence is append-only. Reserving unrelated task numbers for later-stage reviews was rejected because it destroys task/evidence traceability. Removing sensitive-path review requirements for split stages was rejected because it would create a direct bypass of Task 080. Combining 089a and 089b before their finance dependencies close was rejected because it violates the canonical dependency plan.

## Compatibility impact

Existing `ARCH-NNN` records and filenames remain valid. The checker accepts an additive optional stage suffix only for future review files. No REST, event, Connector SDK, database, or provider contract is broken by the governance syntax change.

## Migration and data impact

No application database migration or persisted customer data change is introduced. Architecture review files are repository evidence only.

## Security and privacy impact

The change preserves fail-closed trusted-base diff verification, exact sensitive-path coverage, append-only records, accepted-ADR immutability, provider evidence binding, and bounded review content. It does not admit providers, relax import restrictions, or expose credentials/PII.

## Operational impact

Protected CI continues to build the verifier from the exact merge-base. A future `089b` change may add `architecture/reviews/089b-*.json`; a verifier from a merge base containing this ADR/checker can recognize that record as fresh independent evidence. Repositories whose merge base predates this decision cannot use supplemental stage records and therefore fail closed.
