#!/usr/bin/env bash
set -euo pipefail

base_url="${OPENCART_BASE_URL:-http://127.0.0.1:8095}"
token="${TORGNEXA_OPENCART_BRIDGE_TOKEN:-torgnexa-demo-bridge-token-2026}"
base_url="${base_url%/}"
tmp_dir="$(mktemp -d)"
run_id="$(date +%s)-$$"
trap 'rm -rf "$tmp_dir"' EXIT

request() {
  local name="$1" method="$2" route="$3" body="${4:-}" auth="${5:-yes}"
  local target="$tmp_dir/$name.json" status operation query url
  operation="${route%%\?*}"
  query=''
  if [[ "$route" == *\?* ]]; then
    query="${route#*\?}"
  fi
  url="$base_url/index.php?route=extension/torgnexa/api/$operation"
  if [[ -n "$query" ]]; then
    url+="&$query"
  fi
  local -a args=(--silent --show-error --output "$target" --write-out '%{http_code}' --request "$method")
  if [[ "$auth" == yes ]]; then
    args+=(--header "Authorization: Bearer $token")
  fi
  if [[ -n "$body" ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  status="$(curl "${args[@]}" "$url")"
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

echo "OpenCart bridge smoke: $base_url"
request health_unauthorized GET health '' no
assert_status health_unauthorized 401

request health GET health
assert_status health 200
assert_field health api_version v1

request products GET 'products?page=1&limit=100'
assert_status products 200
assert_field products page 1
python3 - "$tmp_dir/products.json" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))["items"]
skus = {item["sku"] for item in items}
required = {"DEMO-COFFEE-001", "DEMO-TEA-002"}
missing = required - skus
if missing:
    raise SystemExit(f"missing demo SKUs: {sorted(missing)}")
print("PASS products (demo SKUs present)")
PY

request product_by_sku GET 'product-by-sku?sku=DEMO-COFFEE-001'
assert_status product_by_sku 200
assert_field product_by_sku id 1001
assert_field product_by_sku sku DEMO-COFFEE-001

request variant_before GET 'variant?remote_id=product:1001'
assert_status variant_before 200
assert_field variant_before quantity 24

price_body="{\"remote_id\":\"product:1001\",\"price\":\"1599.90\",\"compare_at\":\"1799.90\",\"currency\":\"USD\",\"idempotency_key\":\"smoke-$run_id-price-1001\"}"
request price PUT variant-price "$price_body"
assert_status price 200
assert_field price price 1599.90
request price_replay PUT variant-price "$price_body"
assert_status price_replay 200
assert_field price_replay price 1599.90
request price_conflict PUT variant-price "{\"remote_id\":\"product:1001\",\"price\":\"1699.90\",\"currency\":\"USD\",\"idempotency_key\":\"smoke-$run_id-price-1001\"}"
assert_status price_conflict 409

request inventory PUT variant-inventory "{\"remote_id\":\"product:1001\",\"quantity\":37,\"idempotency_key\":\"smoke-$run_id-inventory-1001\"}"
assert_status inventory 200
assert_field inventory quantity 37
request variant_after GET 'variant?remote_id=product:1001'
assert_status variant_after 200
assert_field variant_after quantity 37

product_body="{\"sku\":\"DEMO-MUG-003\",\"title\":\"TORGNEXA Demo Mug\",\"description\":\"Synthetic smoke-test product\",\"status\":\"publish\",\"idempotency_key\":\"smoke-$run_id-product-003\"}"
request product_create POST product "$product_body"
assert_status product_create 200
created_id="$(json_field product_create id)"
[[ "$created_id" =~ ^[1-9][0-9]*$ ]] || { echo "FAIL product_create: invalid id" >&2; exit 1; }
echo "PASS product_create (id=$created_id)"
request product_replay POST product "$product_body"
assert_status product_replay 200
assert_field product_replay id "$created_id"

request orders GET 'orders?page=1&limit=100'
assert_status orders 200
python3 - "$tmp_dir/orders.json" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))["items"]
if not any(item["id"] == 9001 for item in items):
    raise SystemExit("demo order 9001 is missing")
print("PASS orders (demo order present)")
PY
request order_status PUT order-status "{\"id\":9001,\"status_remote_id\":2,\"idempotency_key\":\"smoke-$run_id-order-9001\"}"
assert_status order_status 200
assert_field order_status status_remote_id 2
request order GET 'order?id=9001'
assert_status order 200
assert_field order status_remote_id 2

echo "OpenCart bridge smoke: all checks passed"
