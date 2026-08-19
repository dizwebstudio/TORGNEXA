import test from "node:test";
import assert from "node:assert/strict";
import {allowedNavigation, canOpenPath} from "../.repository-test/shell/navigation.js";
import {connectorCatalog} from "../.repository-test/generated/connector-catalog.js";

test("navigation is capability-aware", () => {
  const visible = allowedNavigation(["products.read", "orders.read"]);
  assert.deepEqual(visible.map((item) => item.id), ["dashboard", "catalog", "orders"]);
  assert.equal(canOpenPath("/catalog", ["products.read"]), true);
  assert.equal(canOpenPath("/catalog", ["orders.read"]), false);
});

test("generated connector catalog is sorted, unique and contains marketplace manifests", () => {
  const ids = connectorCatalog.map((entry) => entry.id);
  assert.deepEqual(ids, [...ids].sort());
  assert.equal(new Set(ids).size, ids.length);
  assert.ok(connectorCatalog.filter((entry) => entry.family === "marketplace").length >= 5);
  assert.ok(connectorCatalog.every((entry) => entry.capabilities.length > 0 && entry.authKinds.length > 0));
});

test("unknown or missing capability fails closed", () => {
  assert.equal(canOpenPath("/compliance", ["products.read"]), false);
  assert.equal(canOpenPath("/missing", ["products.read"]), false);
});
