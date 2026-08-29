#!/usr/bin/env python3
"""Small stateful Shopify Admin REST contract double for local smoke tests.

Shopify is SaaS and has no official self-hosted Docker image. This server is
therefore deliberately named a protocol double: it exercises the documented
Admin REST request/response shapes and write reconciliation locally, but it
must never be presented as a Shopify merchant or as live qualification.
"""

from copy import deepcopy
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from urllib.parse import parse_qs, urlsplit


TOKEN = os.environ.get("SHOPIFY_MOCK_TOKEN", "shopify-local-token")
HOST = os.environ.get("SHOPIFY_MOCK_HOST", "0.0.0.0")
PORT = int(os.environ.get("SHOPIFY_MOCK_PORT", "8080"))


def timestamp():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def seed_state():
    now = "2026-08-29T00:00:00Z"
    return {
        "shop": {"id": 9001, "primary_location_id": 4001},
        "locations": [{"id": 4001, "name": "TORGNEXA Demo Warehouse", "active": True}],
        "products": {
            1001: {
                "id": 1001,
                "title": "TORGNEXA Demo Coffee",
                "body_html": "Synthetic Shopify smoke fixture",
                "status": "active",
                "vendor": "TORGNEXA",
                "updated_at": now,
                "variants": [
                    {
                        "id": 2001,
                        "sku": "TORGNEXA-SHOPIFY-001",
                        "price": "1499.90",
                        "compare_at_price": "1599.90",
                        "inventory_item_id": 3001,
                        "updated_at": now,
                    }
                ],
            }
        },
        "inventory": {(3001, 4001): 24},
        "orders": {
            5001: {
                "id": 5001,
                "name": "#1001",
                "financial_status": "paid",
                "fulfillment_status": None,
                "cancelled_at": None,
                "closed_at": None,
                "created_at": now,
                "updated_at": now,
                "line_items": [{"id": 6001, "product_id": 1001, "variant_id": 2001, "quantity": 2}],
            }
        },
        "refunds": {
            5001: [
                {
                    "id": 7001,
                    "created_at": now,
                    "note": "Synthetic smoke refund",
                    "transactions": [{"amount": "10.00"}],
                }
            ]
        },
    }


STATE = seed_state()


def json_bytes(value):
    return json.dumps(value, separators=(",", ":")).encode("utf-8")


class ShopifyMockHandler(BaseHTTPRequestHandler):
    server_version = "TORGNEXA-Shopify-Protocol-Double/1.0"

    def log_message(self, _format, *_args):
        # Never log Authorization headers or request bodies.
        return

    def send_json(self, status, value, headers=None):
        payload = json_bytes(value)
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("X-Request-Id", "torgnexa-shopify-mock")
        self.send_header("X-Shopify-API-Version", "2026-07")
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if urlsplit(self.path).path == "/healthz":
            self.send_json(200, {"ok": True})
            return
        if not self.authorized():
            return
        self.route("GET")

    def do_PUT(self):
        if not self.authorized():
            return
        self.route("PUT")

    def do_POST(self):
        if not self.authorized():
            return
        self.route("POST")

    def authorized(self):
        if self.headers.get("X-Shopify-Access-Token") != TOKEN:
            self.send_json(401, {"errors": "Unauthorized"})
            return False
        return True

    def read_json(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length < 0 or length > 1 << 20:
                return None
            return json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError):
            return None

    def route(self, method):
        parsed = urlsplit(self.path)
        path = parsed.path
        prefix = "/admin/api/2026-07"
        if not path.startswith(prefix):
            self.send_json(404, {"errors": "Not found"})
            return
        suffix = path[len(prefix) :]
        query = parse_qs(parsed.query)

        if method == "GET" and suffix == "/shop.json":
            self.send_json(200, {"shop": deepcopy(STATE["shop"])})
            return
        if method == "GET" and suffix == "/locations.json":
            self.send_json(200, {"locations": deepcopy(STATE["locations"])})
            return
        if method == "GET" and suffix == "/products.json":
            self.send_json(200, {"products": list(deepcopy(STATE["products"]).values())})
            return
        if method == "GET" and suffix.startswith("/products/") and suffix.endswith(".json"):
            product = self.product_from_suffix(suffix, "/products/")
            if product is None:
                self.send_json(404, {"errors": "Product not found"})
            else:
                self.send_json(200, {"product": deepcopy(product)})
            return
        if method == "PUT" and suffix.startswith("/products/") and suffix.endswith(".json"):
            product = self.product_from_suffix(suffix, "/products/")
            payload = self.read_json() or {}
            update = payload.get("product") if isinstance(payload, dict) else None
            if product is None or not isinstance(update, dict):
                self.send_json(422, {"errors": "Invalid product update"})
                return
            for field in ("title", "body_html", "status"):
                if field in update:
                    product[field] = update[field]
            product["updated_at"] = timestamp()
            self.send_json(200, {"product": deepcopy(product)})
            return
        if method == "GET" and suffix.startswith("/variants/") and suffix.endswith(".json"):
            variant = self.variant_from_suffix(suffix, "/variants/")
            if variant is None:
                self.send_json(404, {"errors": "Variant not found"})
            else:
                self.send_json(200, {"variant": deepcopy(variant)})
            return
        if method == "PUT" and suffix.startswith("/variants/") and suffix.endswith(".json"):
            variant = self.variant_from_suffix(suffix, "/variants/")
            payload = self.read_json() or {}
            update = payload.get("variant") if isinstance(payload, dict) else None
            if variant is None or not isinstance(update, dict):
                self.send_json(422, {"errors": "Invalid variant update"})
                return
            for field in ("price", "compare_at_price"):
                if field in update:
                    variant[field] = update[field]
            variant["updated_at"] = timestamp()
            self.send_json(200, {"variant": deepcopy(variant)})
            return
        if method == "GET" and suffix == "/inventory_levels.json":
            try:
                item_id = int(query.get("inventory_item_ids", [""])[0])
                location_id = int(query.get("location_ids", [""])[0])
            except ValueError:
                self.send_json(422, {"errors": "Invalid inventory query"})
                return
            quantity = STATE["inventory"].get((item_id, location_id))
            levels = [] if quantity is None else [{"inventory_item_id": item_id, "location_id": location_id, "available": quantity}]
            self.send_json(200, {"inventory_levels": levels})
            return
        if method == "POST" and suffix == "/inventory_levels/set.json":
            payload = self.read_json() or {}
            try:
                item_id = int(payload["inventory_item_id"])
                location_id = int(payload["location_id"])
                available = int(payload["available"])
            except (KeyError, TypeError, ValueError):
                self.send_json(422, {"errors": "Invalid inventory update"})
                return
            if available < 0:
                self.send_json(422, {"errors": "Quantity cannot be negative"})
                return
            STATE["inventory"][(item_id, location_id)] = available
            self.send_json(200, {"inventory_level": {"inventory_item_id": item_id, "location_id": location_id, "available": available}})
            return
        if method == "GET" and suffix == "/orders.json":
            self.send_json(200, {"orders": list(deepcopy(STATE["orders"]).values())})
            return
        if method == "GET" and suffix.startswith("/orders/") and suffix.endswith("/refunds.json"):
            order_id = self.numeric_segment(suffix, "/orders/", "/refunds.json")
            self.send_json(200, {"refunds": deepcopy(STATE["refunds"].get(order_id, []))})
            return
        if method == "GET" and suffix.startswith("/orders/") and suffix.endswith(".json"):
            order_id = self.numeric_segment(suffix, "/orders/", ".json")
            order = STATE["orders"].get(order_id)
            if order is None:
                self.send_json(404, {"errors": "Order not found"})
            else:
                self.send_json(200, {"order": deepcopy(order)})
            return
        if method == "POST" and suffix.startswith("/orders/"):
            for action, status_field in (("cancel", "cancelled_at"), ("close", "closed_at"), ("open", None)):
                ending = f"/{action}.json"
                if suffix.endswith(ending):
                    order_id = self.numeric_segment(suffix, "/orders/", ending)
                    order = STATE["orders"].get(order_id)
                    if order is None:
                        self.send_json(404, {"errors": "Order not found"})
                        return
                    if status_field is None:
                        order["cancelled_at"] = None
                        order["closed_at"] = None
                    else:
                        order[status_field] = timestamp()
                    order["updated_at"] = timestamp()
                    self.send_json(200, {"order": deepcopy(order)})
                    return
        self.send_json(404, {"errors": "Not found"})

    @staticmethod
    def numeric_segment(path, prefix, suffix):
        try:
            return int(path[len(prefix) : -len(suffix)])
        except (ValueError, TypeError):
            return 0

    @staticmethod
    def product_from_suffix(path, prefix):
        try:
            return STATE["products"].get(int(path[len(prefix) : -len(".json")]))
        except (ValueError, TypeError):
            return None

    @staticmethod
    def variant_from_suffix(path, prefix):
        try:
            variant_id = int(path[len(prefix) : -len(".json")])
        except (ValueError, TypeError):
            return None
        for product in STATE["products"].values():
            for variant in product["variants"]:
                if variant["id"] == variant_id:
                    return variant
        return None


if __name__ == "__main__":
    ThreadingHTTPServer((HOST, PORT), ShopifyMockHandler).serve_forever()
