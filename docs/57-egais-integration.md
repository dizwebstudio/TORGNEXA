# EGAIS Integration

EGAIS is implemented as a regulated `GovernmentConnector`, never as provider-specific Core logic. The connector is optional and enabled only for eligible deployments and business scopes.

## Capabilities

The generic capability set may include:
- organization/location identity mapping;
- document/status read;
- inventory/balance read where the official interface permits it;
- regulated document submission/acknowledgement;
- reconciliation and error retrieval;
- reference-directory synchronization.

Exact EGAIS capabilities are determined from the current official interface during Connector Spec work. Unsupported operations remain explicit `false` capabilities.

## Security and approval

Regulated writes require risk classification, signing/authority validation where applicable, idempotency, immutable audit, dry-run/validation where available and approval policy. Private cryptographic material remains inside Signing Service/HSM/approved crypto provider.

## State correctness

Official statuses are authoritative for remote document state. Scheduled reconciliation compares local projections with EGAIS and creates drift/error cases rather than silently rewriting business documents.
