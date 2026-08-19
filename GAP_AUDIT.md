# Gap audit — P4 go-live repository coverage

This document summarizes current repository coverage, not deployment evidence. Task cards and `tasks/EXECUTION_PLAN.md` remain authoritative.

## Repository implementation

Tasks `001`–`121` are repository-implemented. P0 runtime closure is Task 114, P1 operations closure is Task 115, P2 production qualification is Task 116, P3 transactional warehouse execution/release closure is Task 117, and P4 go-live evidence/publication closure is Task 118.

## Closed cross-cutting gaps

- production worker composition is real rather than a sleep process;
- connector runtime/reconciliation/action execution and Kafka Inbox boundaries are wired;
- connector health, notifications, privacy worker and persistent warehouse state exist;
- warehouse incidents are durable/restart-safe;
- tracked order-item reservations can be transactionally rerouted to an explicit backup with sufficient ATP;
- source physical stock is never fabricated at the backup;
- fulfillment execution has immutable allocation lineage and Outbox evidence;
- runtime qualification is mandatory in the release DAG;
- repository license metadata is resolved as Apache-2.0;
- protected release output can be staged as a non-public draft, independently rebound to Sigstore/SLSA identity and GitHub asset digests, and promoted only from retained P4 PASS evidence;
- hosted branch protection and required-workflow facts are measured through GitHub applied rules rather than asserted locally;
- live connector qualification is performed through the public API without retaining credentials.
- the pre-v1 PostgreSQL development chain is compacted to an 11-file active baseline while the original 74-file lineage remains immutable checksum evidence with a verified one-time development rebaseline path.

## Remaining external/operational execution

These are not missing repository modules and must not be "closed" by source edits alone:

- Task 065 protected OIDC prerelease/signing/provenance plus current vulnerability/image scan evidence;
- Task 080 hosting Ruleset Required Workflow, protected branch and required architecture-reviewer proof;
- full Go 1.26.5 test/vet/check on the exact release revision;
- Docker P3 runtime/load/restart qualification on the selected topology;
- deployment backup/restore and upgrade rehearsal against the real backup/KMS/topology;
- live connector conformance with seller/provider credentials where required.

## Intentional capability limits

Provider write support remains capability-truthful. Yandex Market has qualified `prices.write`, WooCommerce supports its reviewed write surface, while providers whose warehouse/listing semantics cannot be represented safely by the current provider-neutral contract remain read-only rather than receiving guessed generic writes.
