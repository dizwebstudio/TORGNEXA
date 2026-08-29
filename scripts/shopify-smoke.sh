#!/usr/bin/env bash
set -euo pipefail

# Shopify is SaaS and has no official self-hosted Docker store. This smoke can
# target a real non-production dev store or the repository's local protocol
# double. The Docker result proves REST contract/reconciliation only.
base_url="${SHOPIFY_BASE_URL:-}"
token="${SHOPIFY_API_TOKEN:-}"
sku="${SHOPIFY_TEST_SKU:-}"
api_version="${SHOPIFY_API_VERSION:-2026-07}"
allow_http="${SHOPIFY_ALLOW_HTTP:-0}"
insecure_tls="${SHOPIFY_INSECURE_TLS:-0}"
allow_writes="${SHOPIFY_ALLOW_WRITES:-0}"
keep_changes="${SHOPIFY_KEEP_CHANGES:-0}"

if [[ -z "$base_url" || -z "$token" || -z "$sku" ]]; then
  echo "SHOPIFY_BASE_URL, SHOPIFY_API_TOKEN and SHOPIFY_TEST_SKU are required" >&2
  exit 2
fi
if [[ ! "$api_version" =~ ^[0-9]{4}-[0-9]{2}$ ]]; then
  echo "SHOPIFY_API_VERSION must look like YYYY-MM" >&2
  exit 2
fi
python3 -c 'from urllib.parse import urlsplit; import sys; u=urlsplit(sys.argv[1]); assert u.scheme in {"http","https"} and u.netloc and not (u.username or u.password or u.query or u.fragment) and u.path in {"","/"}; assert u.scheme == "https" or (sys.argv[2] == "1" and u.hostname in {"127.0.0.1","localhost","::1"})' "$base_url" "$allow_http" || { echo "SHOPIFY_BASE_URL must be a host URL; HTTPS is required except loopback with SHOPIFY_ALLOW_HTTP=1" >&2; exit 2; }
if [[ ${#token} -lt 8 || ${#token} -gt 4096 || "$token" != "${token//[$'\n\r\t ']/}" ]]; then
  echo "SHOPIFY_API_TOKEN must be a non-whitespace token between 8 and 4096 characters" >&2
  exit 2
fi
[[ "$sku" =~ ^[A-Za-z0-9._-]{1,200}$ ]] || { echo "SHOPIFY_TEST_SKU contains unsupported characters" >&2; exit 2; }
for flag in allow_writes keep_changes; do
  value="${!flag}"
  [[ "$value" == 0 || "$value" == 1 ]] || { echo "SHOPIFY_${flag^^} must be 0 or 1" >&2; exit 2; }
done

api_base="${base_url%/}/admin/api/$api_version"
tmp="$(mktemp -d)"
product_id=""; variant_id=""; inventory_item_id=""; location_id=""
original_title=""; original_body=""; original_status=""; original_price=""; original_compare=""; original_quantity=""
changed_product=0; changed_variant=0; changed_inventory=0; restored=0

request() {
  local name="$1" method="$2" path="$3" body="${4:-}" auth="${5:-yes}" status
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30 --request "$method" --output "$tmp/$name.body" --dump-header "$tmp/$name.headers" --write-out '%{http_code}' --header 'Accept: application/json')
  [[ "$insecure_tls" == 1 ]] && args+=(--insecure)
  [[ "$auth" == yes ]] && args+=(--header "X-Shopify-Access-Token: $token")
  [[ -n "$body" ]] && args+=(--header 'Content-Type: application/json' --data "$body")
  status="$(curl "${args[@]}" "$api_base$path" || true)"
  printf '%s\n' "$status" > "$tmp/$name.status"
}
status_of() { cat "$tmp/$1.status"; }
expect_status() { [[ "$(status_of "$1")" == "$2" ]] || { echo "FAIL $1: expected HTTP $2, got $(status_of "$1")" >&2; exit 1; }; }
expect_json() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1])); assert not v.get("errors"), "error payload"' "$tmp/$1.body" || { echo "FAIL $1: error payload" >&2; exit 1; }; }
expect_expr() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1])); safe={"isinstance":isinstance,"int":int,"any":any,"all":all,"list":list}; assert eval(sys.argv[2], {"__builtins__": {}, **safe}, {"v":v})' "$tmp/$1.body" "$2" || { echo "FAIL $1: response assertion" >&2; exit 1; }; }
expect_api_version() { grep -Eiq "^x-shopify-api-version:[[:space:]]*$api_version[[:space:]]*$" "$tmp/$1.headers" || { echo "FAIL $1: API version header mismatch" >&2; exit 1; }; }
expect_product_values() { python3 -c 'import json,sys; p=json.load(open(sys.argv[1])).get("product",{}); assert p.get("title") == sys.argv[2] and p.get("body_html","") == sys.argv[3] and p.get("status") == sys.argv[4]' "$tmp/$1.body" "$2" "$3" "$4" || { echo "FAIL $1: product values were not reconciled" >&2; exit 1; }; }
expect_variant_values() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1])).get("variant",{}); assert v.get("price") == sys.argv[2] and v.get("compare_at_price","") == sys.argv[3]' "$tmp/$1.body" "$2" "$3" || { echo "FAIL $1: variant values were not reconciled" >&2; exit 1; }; }
expect_inventory_quantity() { python3 -c 'import json,sys; x=json.load(open(sys.argv[1])).get("inventory_levels",[]); assert len(x)==1 and x[0].get("available") == int(sys.argv[2])' "$tmp/$1.body" "$2" || { echo "FAIL $1: inventory quantity was not reconciled" >&2; exit 1; }; }

restore_changes() {
  [[ "$allow_writes" == 1 && "$restored" == 0 ]] || return 0
  local ok=0
  set +e
  if [[ "$changed_inventory" == 1 ]]; then
    request restore_inventory POST /inventory_levels/set.json "$(python3 -c 'import json,sys; print(json.dumps({"inventory_item_id":int(sys.argv[1]),"location_id":int(sys.argv[2]),"available":int(sys.argv[3])},separators=(",",":")))' "$inventory_item_id" "$location_id" "$original_quantity")"
    expect_status restore_inventory 200 || ok=1
  fi
  if [[ "$changed_variant" == 1 ]]; then
    request restore_variant PUT "/variants/$variant_id.json" "$(python3 -c 'import json,sys; print(json.dumps({"variant":{"id":int(sys.argv[1]),"price":sys.argv[2],"compare_at_price":sys.argv[3]}},separators=(",",":")))' "$variant_id" "$original_price" "$original_compare")"
    expect_status restore_variant 200 || ok=1
  fi
  if [[ "$changed_product" == 1 ]]; then
    request restore_product PUT "/products/$product_id.json" "$(python3 -c 'import json,sys; print(json.dumps({"product":{"id":int(sys.argv[1]),"title":sys.argv[2],"body_html":sys.argv[3],"status":sys.argv[4]}},separators=(",",":")))' "$product_id" "$original_title" "$original_body" "$original_status")"
    expect_status restore_product 200 || ok=1
  fi
  set -e
  [[ "$ok" == 0 ]] && restored=1 || echo "WARN Shopify cleanup failed; inspect the disposable store" >&2
}
cleanup() { local result=$?; restore_changes; rm -rf "$tmp"; exit "$result"; }
trap cleanup EXIT

echo "Shopify Admin REST smoke: $base_url (api=$api_version)"
request unauthorized GET /shop.json "" no
expect_status unauthorized 401
echo "PASS unauthorized (missing access token rejected)"

request shop GET /shop.json
expect_status shop 200; expect_api_version shop; expect_json shop
location_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["shop"]["primary_location_id"])' "$tmp/shop.body")"
expect_expr shop 'isinstance(v.get("shop",{}).get("id"),int) and v["shop"]["id"] > 0 and int(v["shop"].get("primary_location_id",0)) > 0'
echo "PASS health (shop and API version resolved)"

request locations GET /locations.json
expect_status locations 200; expect_json locations; expect_expr locations 'any(x.get("active") and x.get("id",0)>0 for x in v.get("locations",[]))'
echo "PASS locations (active location listed)"

request products GET "/products.json?limit=50"
expect_status products 200; expect_json products
selection="$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); found=next(( (p["id"],x["id"],x["inventory_item_id"]) for p in d.get("products",[]) for x in p.get("variants",[]) if x.get("sku")==sys.argv[2]),None); assert found; print("|".join(map(str,found)))' "$tmp/products.body" "$sku")"
IFS='|' read -r product_id variant_id inventory_item_id <<< "$selection"
echo "PASS catalog (SKU mapped to product/variant/inventory item)"

request product GET "/products/$product_id.json"
expect_status product 200; expect_json product
python3 -c 'import json,sys; p=json.load(open(sys.argv[1]))["product"]; v=next(x for x in p["variants"] if x["id"]==int(sys.argv[2])); assert p.get("title") and p.get("status") in {"active","archived","draft"} and v.get("sku")==sys.argv[3]; json.dump({"title":p["title"],"body_html":p.get("body_html","") or "","status":p["status"],"price":v["price"],"compare_at_price":v.get("compare_at_price","") or ""},open(sys.argv[4],"w"),separators=(",",":"))' "$tmp/product.body" "$variant_id" "$sku" "$tmp/product.values.json"
original_title="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["title"])' "$tmp/product.values.json")"
original_body="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["body_html"])' "$tmp/product.values.json")"
original_status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["status"])' "$tmp/product.values.json")"
original_price="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["price"])' "$tmp/product.values.json")"
original_compare="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["compare_at_price"])' "$tmp/product.values.json")"
echo "PASS product detail (title/status/price are valid)"

request variant GET "/variants/$variant_id.json"
expect_status variant 200; expect_json variant; expect_expr variant "v.get('variant',{}).get('id') == int('$variant_id') and v['variant'].get('inventory_item_id') == int('$inventory_item_id')"
echo "PASS variant detail (inventory_item_id resolved)"

request inventory GET "/inventory_levels.json?inventory_item_ids=$inventory_item_id&location_ids=$location_id"
expect_status inventory 200; expect_json inventory
original_quantity="$(python3 -c 'import json,sys; x=json.load(open(sys.argv[1])).get("inventory_levels",[]); assert len(x)==1 and isinstance(x[0].get("available"),int) and x[0]["available"]>=0; print(x[0]["available"])' "$tmp/inventory.body")"
echo "PASS inventory (quantity=$original_quantity)"

request orders GET "/orders.json?limit=50&status=any&order=updated_at%20asc"
expect_status orders 200; expect_json orders; expect_expr orders 'all(x.get("id",0)>0 and isinstance(x.get("line_items"),list) for x in v.get("orders",[]))'
order_id="$(python3 -c 'import json,sys; x=json.load(open(sys.argv[1])).get("orders",[]); print(x[0]["id"] if x else "")' "$tmp/orders.body")"
if [[ -n "$order_id" ]]; then
  request refunds GET "/orders/$order_id/refunds.json"; expect_status refunds 200; expect_json refunds; expect_expr refunds 'all(x.get("id",0)>0 and isinstance(x.get("transactions"),list) for x in v.get("refunds",[]))'; echo "PASS orders/refunds (order=$order_id)"
else
  echo "PASS orders (empty bounded response)"
fi

if [[ "$allow_writes" == 1 ]]; then
  run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"; new_title="TORGNEXA Shopify smoke $run_id"; new_body="Synthetic TORGNEXA Shopify smoke $run_id"; new_status=draft; [[ "$original_status" == draft ]] && new_status=active
  changed_product=1
  request product_update PUT "/products/$product_id.json" "$(python3 -c 'import json,sys; print(json.dumps({"product":{"id":int(sys.argv[1]),"title":sys.argv[2],"body_html":sys.argv[3],"status":sys.argv[4]}},separators=(",",":")))' "$product_id" "$new_title" "$new_body" "$new_status")"; expect_status product_update 200; echo "PASS product_update (title/body/status)"
  new_price="$(python3 -c 'from decimal import Decimal; import sys; print(format(Decimal(sys.argv[1])+Decimal("0.01"),".2f"))' "$original_price")"; changed_variant=1
  request variant_update PUT "/variants/$variant_id.json" "$(python3 -c 'import json,sys; print(json.dumps({"variant":{"id":int(sys.argv[1]),"price":sys.argv[2],"compare_at_price":sys.argv[3]}},separators=(",",":")))' "$variant_id" "$new_price" "$original_compare")"; expect_status variant_update 200; echo "PASS variant_update (price)"
  new_quantity=$((original_quantity + 1)); changed_inventory=1
  request inventory_update POST /inventory_levels/set.json "$(python3 -c 'import json,sys; print(json.dumps({"inventory_item_id":int(sys.argv[1]),"location_id":int(sys.argv[2]),"available":int(sys.argv[3])},separators=(",",":")))' "$inventory_item_id" "$location_id" "$new_quantity")"; expect_status inventory_update 200; echo "PASS inventory_update (available)"
  request product_after_write GET "/products/$product_id.json"; expect_status product_after_write 200; expect_product_values product_after_write "$new_title" "$new_body" "$new_status"
  request variant_after_write GET "/variants/$variant_id.json"; expect_status variant_after_write 200; expect_variant_values variant_after_write "$new_price" "$original_compare"
  request inventory_after_write GET "/inventory_levels.json?inventory_item_ids=$inventory_item_id&location_ids=$location_id"; expect_status inventory_after_write 200; expect_inventory_quantity inventory_after_write "$new_quantity"
  echo "PASS writes (read-after-write reconciled)"
  if [[ "$keep_changes" == 0 ]]; then
    restore_changes
    request product_after_restore GET "/products/$product_id.json"; expect_status product_after_restore 200; expect_product_values product_after_restore "$original_title" "$original_body" "$original_status"
    request variant_after_restore GET "/variants/$variant_id.json"; expect_status variant_after_restore 200; expect_variant_values variant_after_restore "$original_price" "$original_compare"
    request inventory_after_restore GET "/inventory_levels.json?inventory_item_ids=$inventory_item_id&location_ids=$location_id"; expect_status inventory_after_restore 200; expect_inventory_quantity inventory_after_restore "$original_quantity"
    echo "PASS cleanup (original product/price/stock restored)"
  else
    echo "INFO changes kept (SHOPIFY_KEEP_CHANGES=1)"
  fi
else
  echo "INFO writes skipped (set SHOPIFY_ALLOW_WRITES=1 on a disposable store)"
fi

echo "Shopify Admin REST smoke: all checks passed"
