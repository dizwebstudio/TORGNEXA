# Avito connector

Task 037 implements the first `classified` provider against Avito Business API. The provider exposes only bounded listing, lead/chat, message and listing-stat projections. It never writes TORGNEXA Core models directly.

Qualified baseline: `classified.listings.read`, `classified.leads.read`, `classified.messages.read`, `classified.messages.reply`, `classified.stats.read`.

Listing mutation, price update, deactivation, paid promotion, autoload upload, delivery and orders are deliberately outside Task 037.
