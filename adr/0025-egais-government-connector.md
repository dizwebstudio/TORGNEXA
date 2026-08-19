# ADR 0025: EGAIS Uses GovernmentConnector + Signing/Approval Ports

## Decision
Implement EGAIS as an optional GovernmentConnector. Regulated writes use existing authority/signing/approval/audit infrastructure and remote status reconciliation.

## Consequences
No EGAIS-specific branches enter Core; capability audit is mandatory before implementation.
