# Operation contract — Task 097 Bitrix24 CRM Connector v1

Provider: `bitrix24`; family: `crm`; SDK major: `1`.

Capabilities:
- `crm.entities.read`
- `crm.entities.write`
- `crm.productrows.read`
- `crm.productrows.write`

Supported entity kinds are lead, deal, contact and company. Stable external identity for TORGNEXA-authored records is `originatorId=TORGNEXA` plus host-supplied `originId`. Product-row operations are supported for lead (`L`) and deal (`D`) owners. All remote calls are POST requests to `/rest/<method>` on the configured HTTPS portal host with OAuth bearer authorization.

The connector never uses deprecated `crm.deal.productrows.*` operations and does not read contact/company `fm` multifields in v1.
