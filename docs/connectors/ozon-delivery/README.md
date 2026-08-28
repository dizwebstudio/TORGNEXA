# Ozon Доставка

Repository evidence for the separate logistics-surface Ozon Delivery
connector:

- `spec.md` — Seller API prerequisite and delivery boundary;
- `capability-audit.md` — capabilities admitted by the current runtime;
- `conformance-plan.md` — deterministic SDK and health-check qualification;
- `conformance-report.json` — machine-readable SDK report.

The current runtime stores encrypted Seller API credentials and checks the
warehouse API. Rates, shipment creation, labels and tracking remain closed
until Ozon's current delivery contract is qualified end to end.
