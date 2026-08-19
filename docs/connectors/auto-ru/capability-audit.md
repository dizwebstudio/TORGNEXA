# Auto.ru capability audit

The current official Auto.ru API exposes dealer account, feed settings/tasks/history, dealer offers and additional dealer-management surfaces. Task 038 admits only the minimum vehicle-publication slice needed by TORGNEXA.

Admitted:
- `classified.listings.read` — dealer passenger-car offers through `/user/offers/cars`;
- `classified.publications.write` — manual passenger-car feed task for NEW/USED inventory;
- `classified.publications.status.read` — detail for the asynchronous feed task.

Not admitted:
- direct offer delete/hide/activate;
- paid products/VAS, campaign auctions or automatic promotion schedules;
- chats, call tracking, booking and trade-in leads;
- dealer finance/wallet or Split;
- motorcycles or commercial-vehicle feed families;
- webhook/callback registration;
- catalog/value-prediction APIs.

The omitted surfaces remain unavailable even though the provider offers them. They require separate capability, approval, privacy and reconciliation qualification before admission.

Security boundary:
- outbound authority is fixed to `apiauto.ru`;
- tokens/session IDs are callback-scoped secret material and never appear in normalized errors;
- account identity is verified exactly before health is green;
- request cursors, task IDs, response bodies and source URLs are bounded;
- source feeds and image URLs are HTTPS-only in TORGNEXA, a stricter transport policy than the provider XML format itself requires.
