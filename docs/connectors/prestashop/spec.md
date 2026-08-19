# PrestaShop provider spec

- API: native `/api/` Webservice.
- Auth: Webservice API key via HTTP Basic username, empty password.
- Reads: `output_format=JSON`, explicit `display`, filters, sorting and bounded limits.
- Writes: XML PATCH/POST only.
- Products: base products plus combinations; stable seller reference is required for canonical product projection.
- Price: product base price; combination effective price = base + combination impact.
- Inventory: only `stock_availables.quantity`.
- Orders: `orders` + `order_details`; PII excluded.
- Order status: POST `order_histories` with `id_order_state` and `id_order`.
