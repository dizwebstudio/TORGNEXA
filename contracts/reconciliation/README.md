# Reconciliation contracts

Task 014 publishes bounded evidence contracts for reconciliation runs, drift detections and remediation attempts. These contracts intentionally contain no organization/workspace identifiers supplied by clients, no raw remote payloads, no credentials and no raw remote error text.

Reconciliation is internal control-plane behavior. Scheduled scans, on-demand scans and incremental scans use the same engine and durable run model. Task 013 remains the propagation engine.
