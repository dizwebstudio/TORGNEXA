# Auto.ru connector

Task 038 implements the `auto-ru` classified/vertical adapter for dealer vehicle inventory. The provider is isolated behind Connector SDK v1 and never writes TORGNEXA Core models directly.

Qualified baseline:
- `classified.listings.read`;
- `classified.publications.write`;
- `classified.publications.status.read`.

The implementation covers bounded dealer-offer reads plus NEW/USED passenger-car XML feed submission and asynchronous task status. Direct offer mutation, promotion, auctions, chats, calls, booking, finance, commercial vehicles and motorcycles are deliberately outside Task 038.

See `vehicle-mapping.md` for the intentionally bounded XML profile and `reconciliation.md` for ambiguous-write handling.
