# Legal Entity / Counterparty Core

TORGNEXA requires a provider-neutral master-data domain for organizations, legal entities, individual entrepreneurs, branches, counterparties, bank accounts, contracts and delegated authority references. ERP, EDO, MChD, payments, procurement, settlements and compliance modules must reference this domain rather than copy legal identifiers independently.

## Canonical entities

- `LegalEntity`: legal name, short name, country, registration/tax identifiers, tax status and lifecycle state.
- `IndividualEntrepreneur`: entrepreneur-specific registration identifiers while implementing the same party interface where possible.
- `Branch`: subordinate unit with optional KPP/address/bank details and parent legal entity.
- `Counterparty`: business relationship view over a party; supplier/customer/carrier/operator roles are capabilities/relationships, not separate copies.
- `LegalAddress`, `PostalAddress`, `ContactPoint`.
- `BankAccount`: bank/BIC/correspondent/account metadata; never store card credentials here.
- `Contract`: number/date/effective period/counterparty/organization/currency/status plus external ERP/EDO references.
- `AuthorityReference`: link to MChD/signing authority or other source of delegated powers.

For Russia, fields support INN, KPP, OGRN/OGRNIP and other identifiers as typed identifiers with validation adapters. Core must remain extensible to other jurisdictions.

## Rules

1. Organization/workspace/store are tenancy/operational entities; `LegalEntity` is the legal party and may be shared by multiple workspaces under explicit tenancy rules.
2. Legal identifiers are normalized and uniqueness rules are tenant/jurisdiction aware.
3. Provider-specific counterparty IDs are stored in mapping tables.
4. Changes to legal/bank/contract data are audited and versioned.
5. Sensitive banking and contact data follows privacy/retention policy.
6. MChD/EDO/signing modules reference party IDs and authority IDs, never duplicate legal master data.
7. Merge/deduplication is explicit, reversible through audit evidence and cannot silently reassign regulated documents.

## APIs/events

Expose scoped CRUD/search APIs, relationship lookups and change history. Emit `party.counterparty.updated.v1` only with minimized fields; never broadcast unnecessary bank/contact PII.
