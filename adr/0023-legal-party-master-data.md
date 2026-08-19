# ADR 0023: Canonical Legal Party Master Data

## Decision
Introduce a provider-neutral LegalEntity/Counterparty/Contract/BankAccount domain used by ERP, EDO, MChD, procurement, payments and compliance. Provider IDs remain mappings; legal identifiers are typed/jurisdiction-aware.

## Consequences
Avoids duplicated INN/KPP/contract data and enables consistent audit/deduplication. Changes require privacy and migration review.
