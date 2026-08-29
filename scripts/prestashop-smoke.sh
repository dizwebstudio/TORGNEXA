#!/usr/bin/env bash
set -euo pipefail

base_url="${PRESTASHOP_BASE_URL:-http://127.0.0.1:8097}"
api_key="${TORGNEXA_PRESTASHOP_API_KEY:-0123456789abcdef0123456789abcdef}"
base_url="${base_url%/}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

request() {
  local name="$1" method="$2" route="$3" body="${4:-}" auth="${5:-yes}"
  local target="$tmp_dir/$name.body" status
  local -a args=(--globoff --silent --show-error --output "$target" --write-out '%{http_code}' --request "$method")
  if [[ "$auth" == yes ]]; then
    args+=(--user "$api_key:")
  fi
  if [[ -n "$body" ]]; then
    args+=(--header 'Content-Type: application/xml' --data "$body")
  fi
  status="$(curl "${args[@]}" "$base_url/api/$route")"
  printf '%s\n' "$status" > "$tmp_dir/$name.status"
}

status_of() { cat "$tmp_dir/$1.status"; }
json_field() {
  python3 - "$tmp_dir/$1.body" "$2" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
for part in sys.argv[2].split('.'):
    value = value[int(part)] if isinstance(value, list) else value[part]
print(value)
PY
}
assert_status() {
  local name="$1" expected="$2" actual
  actual="$(status_of "$name")"
  [[ "$actual" == "$expected" ]] || { echo "FAIL $name: expected HTTP $expected, got $actual" >&2; cat "$tmp_dir/$name.body" >&2; exit 1; }
  echo "PASS $name (HTTP $actual)"
}
assert_field() {
  local name="$1" field="$2" expected="$3" actual
  actual="$(json_field "$name" "$field")"
  [[ "$actual" == "$expected" ]] || { echo "FAIL $name: $field expected '$expected', got '$actual'" >&2; exit 1; }
  echo "PASS $name ($field=$actual)"
}

echo "PrestaShop Webservice smoke: $base_url"
request unauthorized GET 'products?output_format=JSON&display=[id]&limit=1' '' no
assert_status unauthorized 401

request products GET 'products?output_format=JSON&display=[id,reference,name,price,active,date_upd]&limit=100'
assert_status products 200
python3 - "$tmp_dir/products.body" <<'PY'
import json
import sys

items = json.load(open(sys.argv[1], encoding="utf-8"))["products"]
refs = {item["reference"] for item in items}
required = {"TORGNEXA-PS-COFFEE", "TORGNEXA-PS-TEA"}
missing = required - refs
if missing:
    raise SystemExit(f"missing demo references: {sorted(missing)}")
print("PASS products (demo references present)")
PY

coffee_id="$(python3 - "$tmp_dir/products.body" <<'PY'
import json, sys
for item in json.load(open(sys.argv[1], encoding="utf-8"))["products"]:
    if item.get("reference") == "TORGNEXA-PS-COFFEE":
        print(item["id"])
        break
else:
    raise SystemExit("coffee product missing")
PY
)"

request product GET "products/$coffee_id?output_format=JSON&display=[id,reference,name,price,active,date_upd]"
assert_status product 200
assert_field product 'products.0.reference' TORGNEXA-PS-COFFEE

request price PATCH "products/$coffee_id" '<?xml version="1.0" encoding="UTF-8"?><prestashop><product><id>'"$coffee_id"'</id><price>1599.90</price></product></prestashop>'
assert_status price 200
request price_read GET "products/$coffee_id?output_format=JSON&display=[id,reference,price]"
assert_status price_read 200
assert_field price_read 'products.0.price' 1599.900000

request stock GET "stock_availables?output_format=JSON&display=[id,id_product,id_product_attribute,quantity]&filter[id_product]=[$coffee_id]&filter[id_product_attribute]=[0]&limit=2"
assert_status stock 200
stock_id="$(json_field stock 'stock_availables.0.id')"
request inventory PATCH "stock_availables/$stock_id" '<?xml version="1.0" encoding="UTF-8"?><prestashop><stock_available><id>'"$stock_id"'</id><quantity>37</quantity></stock_available></prestashop>'
assert_status inventory 200
request stock_read GET "stock_availables/$stock_id?output_format=JSON&display=[id,id_product,id_product_attribute,quantity]"
assert_status stock_read 200
assert_field stock_read 'stock_availables.0.quantity' 37

request orders GET 'orders?output_format=JSON&display=[id,reference,current_state,date_add,date_upd]&limit=10'
assert_status orders 200
echo "PASS orders (official orders resource reachable)"

echo "PrestaShop Webservice smoke: all checks passed"
