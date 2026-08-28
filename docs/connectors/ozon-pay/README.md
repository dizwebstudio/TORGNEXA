# Ozon Pay

Repository evidence for the separate finance-surface Ozon Pay connector:

- `spec.md` — Seller API prerequisite and activation boundary;
- `capability-audit.md` — capabilities admitted by the current runtime;
- `conformance-plan.md` — deterministic SDK and health-check qualification;
- `conformance-report.json` — machine-readable SDK report.

The current runtime stores encrypted `client_id`/`api_key` credentials and
checks Seller API access. Ozon Pay merchant activation and payment mutations
are intentionally not claimed until the current merchant API contract is
qualified.
