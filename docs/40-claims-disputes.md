# Claims, Disputes & Compensation

Normalize post-sale disputes across marketplaces, carriers, PUDO and suppliers.

Entities: Claim, ClaimItem, Evidence, Counterparty, Compensation, Decision, Deadline.

Claim types include lost, damaged, wrong item, shortage, return disagreement, marketplace fee dispute, carrier SLA breach and supplier discrepancy.

Claims maintain a state machine, evidence files in S3, deadline/SLA timers, financial linkage and immutable communication history. Automated submission is capability- and policy-gated.
