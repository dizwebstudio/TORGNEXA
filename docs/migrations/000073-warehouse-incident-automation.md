# Migration 000073 — warehouse incident automation

Phase: `expand`  
Risk: `high`  
Backup required: yes

Adds persistent `warehouse_incidents` and append-only `warehouse_incident_decisions`, both protected by FORCE ROW LEVEL SECURITY. The worker dispatcher gains `warehouse_incident` jobs for incidents in `open`/`processing` state.

The migration stores routing evidence only. It contains no statement that moves an `inventory_positions` row to another warehouse or creates destination stock. Migration 000072 remains the authoritative allocation guard during mixed-version rollout.

Rollback is application-first: stop binaries that create/claim incident jobs, restore the previous dispatch functions/constraint in a contract migration only after the fleet is old-version compatible, and retain incident evidence according to operations/audit policy. Do not drop evidence during an active incident.
