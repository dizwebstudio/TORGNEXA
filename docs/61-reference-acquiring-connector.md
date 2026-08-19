# Reference Acquiring Connector

The generic `PaymentProvider` must be proven against one production-grade card/acquiring provider in addition to the SBP baseline. The initial reference implementation should target a widely used Russian provider selected in the task after a current API/terms/capability audit (default candidate: YooKassa).

## Required reference capabilities

- create/confirm payment where supported;
- payment status and durable webhook processing;
- full/partial refund;
- idempotency keys;
- commission/settlement metadata available through the provider;
- reconciliation against orders and settlement ledger;
- test/sandbox fixtures where officially available.

Provider-specific fields remain inside connector config/mapping/extension metadata. Core payment state is normalized.

## PCI boundary

TORGNEXA never stores PAN/CVV or equivalent raw payment credentials. Hosted/provider tokenization is preferred. Logs/events contain only safe provider/payment references.
