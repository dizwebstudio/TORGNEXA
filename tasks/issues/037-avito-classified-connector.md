# Task 037: Avito classified connector

## Objective
Implement ClassifiedConnector baseline for listings/leads/messages/stats where officially permitted.

## Dependencies
010, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Distinct classified model; provider IDs; read/write risk classes; fixtures.

Run required repository checks and report results, risks and follow-ups.

## Completion — 2026-08-11

Status: **repository-complete**.

- Added a first-class classified SDK surface instead of reusing marketplace Product/Order projections.
- Registered `avito` with exact `api.avito.ru` authority, OAuth2 secret boundary and numeric account identity verification through `/core/v1/accounts/self`.
- Admitted listing reads, lead/chat reads, message reads, text replies and listing statistics only.
- Classified reads are `read`; message reply is `write_sensitive`. Listing mutations, paid promotion, autoload and other destructive/provider-commercial surfaces remain undeclared.
- Ambiguous message-write transport/5xx results fail closed as non-retryable `write_outcome_unknown` because this qualified send baseline has no caller-supplied remote idempotency key.
- Added deterministic fixtures/tests, provider docs, Task-064 evidence and `ARCH-037`.

Next extended-channel task: `038 Auto.ru Connector`.
