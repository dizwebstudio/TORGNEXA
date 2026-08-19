# Task 095 — PrestaShop Connector Contract v1

PrestaShop is a storefront provider behind Connector SDK v1. The frozen root Connector/Runtime interfaces do not change.

## Credentials and transport
Each account binds to one validated HTTPS store host, optional safe base path, one language ID and optional shop ID. The Webservice API key is used as HTTP Basic username with an empty password and exists only within a SecretAccessor callback.

## Reads
Products and combinations are read from native Webservice resources using bounded `limit=offset,count`. JSON response output is requested explicitly. Prices are exact decimal projections. Available quantity comes only from `stock_availables`. Orders are projected without customer PII; line items are read from `order_details`.

## Writes
PrestaShop Webservice JSON input is not used. Partial mutations use XML PATCH. Inventory updates target the exact StockAvailable resource. Product/base and combination prices are desired-state writes; combination price impact is calculated exactly. Order status uses a new `order_history` resource. Ambiguous network/server outcomes are reconciled by reads before success and otherwise return `write_outcome_unknown`.
