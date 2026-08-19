# Magnit Market Conformance Plan

Task 035 must pass the canonical Task 064 thirteen-check suite using only synthetic credentials and fixtures.

Additional provider-specific checks:

1. manifest grants only `products.read`, `prices.read`, `inventory.read`, `orders.read`;
2. `X-Api-Key` bytes exist only inside `SecretAccessor.UseSecret` callback;
3. health verifies configured shop through `/api/seller/v1/shops`;
4. product page is shop-scoped and joins official price timestamps one-to-one;
5. price pagination is shop-scoped official `last_key` keyset pagination;
6. exact money rejects exponent notation and malformed/duplicate/missing price rows;
7. inventory rejects invented physical warehouse IDs and impossible reserved/stock arithmetic;
8. order cursor freezes the `created_at` window across remote `next_page_token` pages;
9. buyer ID and delivery region never enter the normalized order projection;
10. response/cursor bounds and configuration fingerprints fail closed;
11. raw API response/transport/secret text is absent from normalized failures;
12. architecture admission imports only Connector SDK and standard library;
13. Linux sandbox qualification passes with production credential rejection and egress/resource controls.
