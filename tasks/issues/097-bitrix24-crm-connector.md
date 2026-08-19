# Task 097: Bitrix24 CRM Connector

## Status
`repository-complete` — 2026-08-12.

## Objective
Add Bitrix24 as the first provider in a provider-neutral CRM connector family, using current universal `crm.item.*` and `crm.item.productrow.*` REST methods rather than deprecated entity-specific list/write APIs.

## Dependencies
010, 013, 014, 017, 021, 025, 030, 060, 064

## Deliverables
- additive `crm` Connector SDK family and capability registry;
- OAuth2 bearer authentication with exact HTTPS portal-host binding and callback-scoped token access;
- bounded reads for leads, deals, contacts and companies;
- idempotent/reconciled create/update using `originatorId=TORGNEXA` + `originId` as the stable external identity;
- deal/lead product-row reads and full-set replacement through `crm.item.productrow.*`;
- exact decimal projection for opportunity, price, quantity and tax fields;
- deterministic tests, capability audit, provider spec, conformance candidate and architecture review.

## Acceptance
1. Frozen root `Connector`, `Runtime` and `SecretAccessor` interfaces remain unchanged.
2. `crm` is a first-class provider-neutral family; no Bitrix24 name enters Core workflows.
3. OAuth access token is never placed in URL/query/body or persisted outside `SecretAccessor` callback scope.
4. New code uses universal `crm.item.*`; deprecated `crm.deal.list`, `crm.contact.list`, `crm.company.list` and deal product-row APIs are not used.
5. Fixed Bitrix24 50-record pages support arbitrary TORGNEXA page limits without dropping records.
6. Ambiguous entity writes reconcile by remote ID or TORGNEXA origin identity before success; unprovable outcomes fail closed.
7. Product-row replacement reads current state first and verifies the final set after mutation.
8. Contact multifields (`fm`: phone/e-mail/messengers) are deliberately not read in v1 because Bitrix24 requires wildcard selection to return them; v1 avoids unintentionally ingesting unrelated fields/PII.

## Deferred deliberately
OAuth refresh/installation lifecycle, local incoming-webhook authentication, contact/company multifields, activities/timeline, tasks, telephony, invoices/payments, custom SPA schemas and event subscriptions are follow-up tasks.
