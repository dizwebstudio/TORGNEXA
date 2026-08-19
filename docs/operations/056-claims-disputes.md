# Claims & Disputes

Task `056` implementation lives in `internal/platform/claims`.

## Safety invariants

Claims retain released-upload evidence references, financial linkage, deadlines and escalation evidence. Unreleased uploads cannot become dispute evidence.

## Persistence

PostgreSQL expand migration: `000035_claims_disputes.sql`. In-memory implementations in tests are reference semantics, not production durability.
