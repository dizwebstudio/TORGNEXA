import test from "node:test";
import assert from "node:assert/strict";
import {decodeOrderPage, decodeProductPage, decodeReturnDetails, decodeReturnPage} from "../.repository-test/api/decoders.js";

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

test("return decoder preserves lifecycle, quantities and disposition", () => {
  const summary = {id: "ret-1", order_id: "ord-1", status: "inspecting", reason_code: "wrong_size", source: "api.returns", currency: "RUB", requested_shipping_minor: 199, requested_tax_minor: 0, version: 3, created_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T11:00:00Z"};
  const page = decodeReturnPage({items: [summary]});
  assert.equal(page.items[0]?.status, "inspecting");
  const details = decodeReturnDetails({return: summary, items: [{id: "line-1", return_id: "ret-1", order_item_id: "line-order-1", unit: "PCS", disposition: "restock", requested: {coefficient: 2, scale: 0, unit: "PCS"}, received: {coefficient: 1, scale: 0, unit: "PCS"}, accepted: {coefficient: 1, scale: 0, unit: "PCS"}, version: 1, created_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z"}]});
  assert.equal(details.items[0]?.accepted.coefficient, 1);
  assert.equal(details.items[0]?.disposition, "restock");
});

test("return decoder rejects malformed quantities and money", () => {
  const summary = {id: "ret-1", order_id: "ord-1", status: "requested", reason_code: "customer_changed_mind", source: "api.returns", currency: "RUB", requested_shipping_minor: 0, requested_tax_minor: 0, version: 1, created_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z"};
  assert.throws(() => decodeReturnPage({items: [{...summary, currency: "rub"}]}), /invalid return/);
  assert.throws(() => decodeReturnDetails({return: summary, items: [{id: "line-1", return_id: "ret-1", order_item_id: "line-order-1", unit: "pcs", disposition: "restock", requested: {coefficient: 1, scale: 0, unit: "PCS"}, received: {coefficient: 0, scale: 0, unit: "PCS"}, accepted: {coefficient: 0, scale: 0, unit: "PCS"}, version: 1, created_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z"}]}), /invalid return item/);
});
