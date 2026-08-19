# Avito capability audit

Admitted:
- `classified.listings.read` — current Items API list/read surface.
- `classified.leads.read` — Messenger chat list as lead projection.
- `classified.messages.read` — bounded chat-message history.
- `classified.messages.reply` — text reply only; sensitive write.
- `classified.stats.read` — listing counters only.

Not admitted: listing create/edit/price/deactivate, image messages, message delete, webhook subscription, autoload, promotion/VAS, delivery/orders, finance, calls or ratings. These require separate current-contract and risk qualification.

The connector treats HTTP 402 on Messenger as `subscription_required`; current Avito commercial access to message read/send may require an API Messenger subscription. No raw provider error body is propagated.
