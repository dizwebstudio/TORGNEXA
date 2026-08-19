import test from "node:test";
import assert from "node:assert/strict";
import {decodeOrderPage, decodeProductPage} from "../.repository-test/api/decoders.js";

test("product decoder accepts bounded public projection", () => {
  const page = decodeProductPage({items: [{id: "p1", code: "SKU-1", title: "Bolt", status: "active", updated_at: "2026-08-10T10:00:00Z"}], next_cursor: "v1.abc"});
  assert.equal(page.items[0]?.code, "SKU-1");
  assert.equal(page.next_cursor, "v1.abc");
});

test("order decoder rejects malformed money projection", () => {
  assert.throws(() => decodeOrderPage({items: [{id: "o1", order_number: "N1", status: "pending", currency: "RUB", grand_minor_units: -1, placed_at: "2026-08-10T10:00:00Z", updated_at: "2026-08-10T10:00:00Z"}]}), /order hit/);
});
