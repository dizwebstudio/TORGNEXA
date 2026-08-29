import test from "node:test";
import assert from "node:assert/strict";
import {decodeOrderPage, decodeProductPage} from "../.repository-test/api/decoders.js";

test("product decoder accepts bounded public projection", () => {
  const page = decodeProductPage({items: [{id: "p1", code: "SKU-1", title: "Bolt", description: "Compact steel bolt", status: "active", updated_at: "2026-08-10T10:00:00Z", image_url: "https://images.example.test/bolt.jpg", price: {minor_units: 129900, currency: "RUB"}}], next_cursor: "v1.abc"});
  assert.equal(page.items[0]?.code, "SKU-1");
  assert.equal(page.items[0]?.image_url, "https://images.example.test/bolt.jpg");
  assert.equal(page.items[0]?.description, "Compact steel bolt");
  assert.deepEqual(page.items[0]?.price, {minor_units: 129900, currency: "RUB"});
  assert.equal(page.next_cursor, "v1.abc");
});

test("product decoder rejects malformed optional price", () => {
  assert.throws(() => decodeProductPage({items: [{id: "p1", code: "SKU-1", title: "Bolt", status: "active", updated_at: "2026-08-10T10:00:00Z", price: {minor_units: -1, currency: "RUB"}}]}), /product price/);
});

test("order decoder rejects malformed money projection", () => {
  assert.throws(() => decodeOrderPage({items: [{id: "o1", order_number: "N1", status: "pending", currency: "RUB", grand_minor_units: -1, placed_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z"}]}), /order hit/);
});

test("order decoder keeps optional product preview fields", () => {
  const page = decodeOrderPage({items: [{id: "o1", order_number: "N1", status: "pending", currency: "RUB", grand_minor_units: 129900, placed_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z", product_title: "AirBeat X5", product_sku: "DEMO-SKU", product_image_url: "https://images.example.test/airbeat.jpg"}]});
  assert.equal(page.items[0]?.product_title, "AirBeat X5");
  assert.equal(page.items[0]?.product_sku, "DEMO-SKU");
  assert.equal(page.items[0]?.product_image_url, "https://images.example.test/airbeat.jpg");
});
