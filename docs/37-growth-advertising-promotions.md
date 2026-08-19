# Growth, Advertising & Promotions

Growth is a first-class domain because marketplace advertising, promotion calendars, discounts and social distribution directly affect price, margin and inventory.

## Domain

- Campaign, AdGroup, Placement, Creative, Bid, Budget;
- Promotion, Discount, Coupon, PromoParticipation;
- PricingRule, FloorPrice, MarginGuard;
- attribution links from social/publication/campaign to traffic/order/revenue;
- normalized metrics: impressions, clicks, spend, orders, revenue, ROAS/ROMI/DRR.

## Safety

Advertising spend, bid changes, floor-price violations and mass promotion participation are write-sensitive operations. They require limits, dry-run where possible, approval policies and full audit.

## Connector design

Marketplace/social connectors expose capabilities rather than provider-specific branching, for example `ads.read`, `ads.manage`, `promotion.read`, `promotion.manage`, `pricing.write`.

Reporting must distinguish source facts from calculated attribution assumptions.
