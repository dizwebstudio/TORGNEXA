# ADR 0066: Chestny ZNAK read/status baseline

## Status
Accepted — Task 66.

## Context
Task 068 needs a government marking boundary without copying raw marking codes into general-purpose storage or pretending undocumented writes are safe. The official GIS MT/True API exposes read/status and product/reference operations, while authoritative marking state remains external.

## Decision
1. Admit `chestny-znak` through Connector SDK v1 for `marking.status.read`, `government.references.read` and `government.reconciliation.run` only.
2. Hash marking codes before producing host observations; host evidence stores only fingerprints and normalized state.
3. Treat remote status as authoritative during reconciliation.
4. Keep regulated write capabilities disabled until a separately reviewed official write contract, approval policy and signing path are qualified.

## Alternatives considered
- Store raw codes in host ledgers: rejected because it enlarges regulated-data exposure.
- Add provider-specific logic to Core: rejected by the frozen connector boundary.
- Enable guessed write endpoints: rejected; regulated writes fail closed.

## Compatibility impact
The change is additive. Existing catalog/order/marking contracts are not rewritten and Connector SDK root interfaces remain unchanged.

## Migration and data impact
Migration `000041` adds tenant-scoped marking status facts and reconciliation evidence with forced RLS and append-only status evidence. No existing row is rewritten.

## Operational impact
Deployments must bind the typed transport to an approved official account, configure egress/certificates, monitor reconciliation lag and retain evidence according to Task 061 policy.

## Security and privacy impact
Raw marking codes are not persisted by the host module; fingerprints are used for durable correlation. Secrets/certificates remain host-owned and provider code has no SQL/Core authority.

## Consequences
TORGNEXA gains a safe read/reconcile baseline for marking. Publication/write workflows remain intentionally unavailable until separately qualified.
