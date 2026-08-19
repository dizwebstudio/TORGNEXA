# Contributing

Pick one task; read AGENTS + relevant ADRs/contracts; update tests and docs; run
`./scripts/check.sh`; never commit production credentials or payloads.

Changes under protected Core/Platform/process or architecture paths require a
new exact `architecture/reviews/NNN-*.json` record. New domains/providers run
the complete gap audit. Frozen-pillar, gate, and mixed provider/Core changes add
a new/superseding ADR. Provider admission is closed until the policy's required
foundation tasks pass.
