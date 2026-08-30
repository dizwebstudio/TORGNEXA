import test from "node:test";
import assert from "node:assert/strict";
import {allowedNavigation, canOpenPath, isKnownPath, navigationItems} from "../.repository-test/shell/navigation.js";
import {connectorCatalog} from "../.repository-test/generated/connector-catalog.js";

test("navigation is capability-aware", () => {
  const visible = allowedNavigation(["products.read", "orders.read"]);
  assert.deepEqual(visible.map((item) => item.id), ["dashboard", "catalog", "publication-quality", "orders"]);
  assert.equal(canOpenPath("/catalog", ["products.read"]), true);
  assert.equal(canOpenPath("/catalog", ["orders.read"]), false);
});

test("generated connector catalog is sorted, unique and contains marketplace manifests", () => {
  const ids = connectorCatalog.map((entry) => entry.id);
  assert.deepEqual(ids, [...ids].sort());
  assert.equal(new Set(ids).size, ids.length);
  assert.ok(connectorCatalog.filter((entry) => entry.family === "marketplace").length >= 5);
  assert.ok(connectorCatalog.every((entry) => entry.capabilities.length > 0 && entry.authKinds.length > 0));
  assert.ok(connectorCatalog.filter((entry) => entry.family === "marketplace").every((entry) => entry.presentation?.logo.startsWith("/connector-logos/")));
});

test("unknown or missing capability fails closed", () => {
  assert.equal(canOpenPath("/compliance", ["products.read"]), false);
  assert.equal(canOpenPath("/missing", ["products.read"]), false);
});

test("navigation shortcuts are unique and unknown nested routes are 404 candidates", () => {
  const shortcuts = navigationItems.map((item) => item.shortcut).filter(Boolean);
  assert.equal(new Set(shortcuts).size, shortcuts.length);
  assert.equal(navigationItems.find((item) => item.id === "orders")?.shortcut, "G O");
  assert.equal(isKnownPath("/orders/order-1"), true);
  assert.equal(isKnownPath("/orders/order-1/unknown"), false);
  assert.equal(isKnownPath("/missing"), false);
});
