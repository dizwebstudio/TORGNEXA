# ADR 0046: Provider-neutral entitlements and atomic quotas

Status: Accepted

## Context
Feature access and tenant quotas are needed by connectors, developer APIs, notifications, Cloud and Enterprise capabilities. Hard-coded branches such as `if plan == enterprise` would couple Community runtime to commercial billing and scatter authorization semantics across modules.

## Decision
Task 028 adds `internal/platform/entitlements`, `entitlementguard`, and PostgreSQL `entitlementrepo`. Feature entitlements and quota policies are append-only versioned tenant records addressed only by stable feature/metric keys plus a provider-neutral source. Missing feature rules and missing quota policies fail closed. Quota usage is idempotent and enforced atomically per UTC lifetime/day/month bucket with transaction-scoped advisory locking and a locked counter row.

Community deployments may provision local rules/policies directly. Future Task 086 Cloud Billing may synchronize the same contracts but is not a runtime dependency. No plan names or billing-provider conditions are part of the evaluation API.

## Consequences
All future capability checks can use one host-side guard. Quota consumers must provide immutable usage IDs and correlation IDs. Usage evidence is append-only while counters are mutable derived enforcement state. Changes to commercial plans translate into entitlement/quota records rather than application branches.

## Alternatives considered
Plan enums in Core were rejected because they contaminate Community and make future offerings breaking changes. Pure in-memory counters were rejected because restarts and concurrency can exceed limits. A single boolean feature flag table was rejected because it cannot safely model quotas or historical effective policy versions.

## Compatibility impact
The change is additive: new API/contracts/events and new platform modules. No published billing subscription contract is modified. Task 086 remains a later independent Cloud lifecycle.

## Migration and data impact
Expand migration `000018_entitlements.sql` adds versioned entitlement/quota policy tables, mutable quota counters and append-only idempotent usage evidence. Existing tables are not renamed or dropped.

## Security and privacy impact
Forced RLS binds every rule/policy/counter/usage row to organization/workspace scope. Evaluation is deny-by-default and the API derives scope from authentication rather than request payload. Usage evidence stores metric/amount/correlation only and no customer content or secrets.

## Operational impact
Quota windows are defined in UTC and are deterministic across locales/DST. Counters require database availability for strict consumption; callers must fail closed rather than bypass quota on repository failure. Backup/restore must preserve both usage evidence and counters.
