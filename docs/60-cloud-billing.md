# TORGNEXA Cloud Billing

Cloud billing is separate from marketplace/customer payments. It monetizes TORGNEXA Cloud/managed Enterprise plans while Community remains usable without the cloud billing service.

## Model

- `Plan` and versioned plan catalog;
- `Subscription` with lifecycle state;
- `EntitlementGrant` derived from plan/add-ons/contracts;
- `UsageMeter` and immutable usage records;
- `Invoice`/credit/adjustment;
- provider-neutral SaaS payment reference;
- grace period, suspension, reactivation, cancellation and renewal state.

Lifecycle: `trial/active -> past_due -> grace -> suspended -> active|cancelled`, with configurable policy and manual enterprise contracts.

## Separation of concerns

Entitlements answer whether a feature/limit is allowed. Billing computes commercial obligations. A billing outage must not corrupt commerce state; grace behavior is explicit and audited.

## Metering

Meter connector accounts, active stores, API/automation usage, storage and optional premium capabilities. Metering is idempotent, tenant-scoped and replayable from source events where feasible.

## Security

Do not store raw card credentials. Cloud billing uses the generic PaymentProvider/reference-acquirer path or an external billing provider. Invoice/payment records are auditable and reconciled.
