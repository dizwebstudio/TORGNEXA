#!/usr/bin/env bash
set -euo pipefail

api_base="${WOOCOMMERCE_API_BASE_URL:-https://127.0.0.1:8446/wp-json/wc/v3}"
api_base="${api_base%/}"
consumer_key="${WOO_CONSUMER_KEY:-ck_torgnexa_demo_20260829_000000000000000000}"
consumer_secret="${WOO_CONSUMER_SECRET:-cs_torgnexa_demo_20260829_0000000000000000}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

request() {
  local name="$1" method="$2" path="$3" body="${4:-}" auth="${5:-yes}"
  local target="$tmp_dir/$name.json" status
  local -a args=(--insecure --location --silent --show-error --output "$target" --write-out '%{http_code}' --request "$method")
  if [[ "$auth" == yes ]]; then
    args+=(--user "$consumer_key:$consumer_secret")
  fi
  if [[ -n "$body" ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  status="$(curl "${args[@]}" "$api_base/$path")"
  printf '%s\n' "$status" > "$tmp_dir/$name.status"
}

status_of() { cat "$tmp_dir/$1.status"; }
json_field() {
  python3 - "$tmp_dir/$1.json" "$2" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    value = json.load(handle)
for part in sys.argv[2].split('.'):
    value = value[int(part)] if isinstance(value, list) else value[part]
print(value)
PY
}
assert_status() {
  local name="$1" expected="$2" actual
  actual="$(status_of "$name")"
  [[ "$actual" == "$expected" ]] || { echo "FAIL $name: expected HTTP $expected, got $actual" >&2; cat "$tmp_dir/$name.json" >&2; exit 1; }
  echo "PASS $name (HTTP $actual)"
}
assert_field() {
  local name="$1" field="$2" expected="$3" actual
  actual="$(json_field "$name" "$field")"
  [[ "$actual" == "$expected" ]] || { echo "FAIL $name: $field expected '$expected', got '$actual'" >&2; exit 1; }
  echo "PASS $name ($field=$actual)"
}

echo "WooCommerce REST smoke: $api_base"
request unauthorized GET 'products?per_page=1' '' no
assert_status unauthorized 401

request products GET 'products?per_page=100&orderby=id&order=asc'
assert_status products 200
python3 - "$tmp_dir/products.json" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))
skus = {item.get("sku") for item in items}
required = {"TORGNEXA-WOO-COFFEE", "TORGNEXA-WOO-TEA"}
missing = required - skus
if missing:
    raise SystemExit(f"missing demo SKUs: {sorted(missing)}")
print("PASS products (demo SKUs present)")
PY

coffee_id="$(python3 - "$tmp_dir/products.json" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))
for item in items:
    if item.get("sku") == "TORGNEXA-WOO-COFFEE":
        print(item["id"])
        break
else:
    raise SystemExit("coffee product not found")
PY
)"
request product_by_sku GET 'products?sku=TORGNEXA-WOO-COFFEE&per_page=2'
assert_status product_by_sku 200
assert_field product_by_sku 0.id "$coffee_id"
assert_field product_by_sku 0.sku TORGNEXA-WOO-COFFEE

update_body="{\"name\":\"TORGNEXA Demo Coffee Updated\",\"regular_price\":\"1599.90\",\"manage_stock\":true,\"stock_quantity\":37}"
request product_update PUT "products/$coffee_id" "$update_body"
assert_status product_update 200
assert_field product_update id "$coffee_id"
assert_field product_update regular_price 1599.90
assert_field product_update stock_quantity 37

request product_after GET "products/$coffee_id"
assert_status product_after 200
assert_field product_after name "TORGNEXA Demo Coffee Updated"
assert_field product_after stock_quantity 37

request orders GET 'orders?per_page=100&orderby=id&order=asc'
assert_status orders 200
order_id="$(python3 - "$tmp_dir/orders.json" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))
for item in items:
    if any(line.get("sku") == "TORGNEXA-WOO-COFFEE" for line in item.get("line_items", [])):
        print(item["id"])
        break
else:
    raise SystemExit("demo order with coffee line item is missing")
PY
)"
echo "PASS orders (demo order $order_id present)"

request order_status PUT "orders/$order_id" "{\"status\":\"completed\"}"
assert_status order_status 200
assert_field order_status id "$order_id"
assert_field order_status status completed

request order_after GET "orders/$order_id"
assert_status order_after 200
assert_field order_after status completed

request refunds GET "orders/$order_id/refunds"
assert_status refunds 200
python3 - "$tmp_dir/refunds.json" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
if not isinstance(value, list):
    raise SystemExit("refund response is not an array")
print("PASS refunds (returns endpoint responds)")
PY

echo "WooCommerce REST smoke: all checks passed"
