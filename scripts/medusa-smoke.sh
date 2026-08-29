#!/usr/bin/env bash
set -euo pipefail

# Credentialed Medusa v2 Admin REST smoke. This is separate from SDK
# conformance and targets only an operator-provided non-production store.
# Medusa secret API keys use the raw token in `Authorization: Basic` (not
# RFC-7617 user:password and not a base64 value).
base_url="${MEDUSA_BASE_URL:-}"
token="${MEDUSA_API_TOKEN:-}"
sku="${MEDUSA_TEST_SKU:-}"
order_id="${MEDUSA_TEST_ORDER_ID:-}"
currency="${MEDUSA_STORE_CURRENCY:-USD}"
allow_http="${MEDUSA_ALLOW_HTTP:-0}"
insecure_tls="${MEDUSA_INSECURE_TLS:-0}"
allow_writes="${MEDUSA_ALLOW_WRITES:-0}"
keep_changes="${MEDUSA_KEEP_CHANGES:-0}"

if [[ -z "$base_url" || -z "$token" || -z "$sku" ]]; then
  cat >&2 <<'EOF'
Medusa live smoke requires MEDUSA_BASE_URL, MEDUSA_API_TOKEN and MEDUSA_TEST_SKU.
Use a secret API key from a dedicated non-production Medusa v2 store and an
existing synthetic SKU; product creation is intentionally unsupported here.
EOF
  exit 2
fi
base_url="${base_url%/}"
python3 - "$base_url" "$allow_http" <<'PY'
from urllib.parse import urlsplit
import sys
u = urlsplit(sys.argv[1])
if u.scheme not in {"http", "https"} or not u.netloc:
    raise SystemExit("MEDUSA_BASE_URL must be an absolute HTTP(S) URL")
if u.username or u.password or u.query or u.fragment:
    raise SystemExit("MEDUSA_BASE_URL must not contain credentials, query or fragment")
if u.path.rstrip("/").endswith("/admin"):
    raise SystemExit("MEDUSA_BASE_URL must be the store root, without /admin")
if u.scheme != "https" and sys.argv[2] != "1":
    raise SystemExit("HTTPS is required; set MEDUSA_ALLOW_HTTP=1 only for an isolated local store")
if u.scheme == "http" and u.hostname not in {"127.0.0.1", "localhost", "::1"}:
    raise SystemExit("plain HTTP is restricted to loopback; use HTTPS for a remote store")
PY
if [[ ${#token} -lt 8 || ${#token} -gt 4096 || "$token" != "${token//[$'\n\r\t ']/}" ]]; then
  echo "MEDUSA_API_TOKEN must be a non-whitespace token between 8 and 4096 characters" >&2; exit 2
fi
if [[ ! "$sku" =~ ^[A-Za-z0-9._-]{1,200}$ ]]; then
  echo "MEDUSA_TEST_SKU must contain only ASCII letters, digits, dot, underscore or hyphen" >&2; exit 2
fi
if [[ -n "$order_id" && ! "$order_id" =~ ^[A-Za-z0-9._-]{1,128}$ ]]; then
  echo "MEDUSA_TEST_ORDER_ID contains unsupported characters" >&2; exit 2
fi
if [[ ! "$currency" =~ ^[A-Z]{3}$ ]]; then
  echo "MEDUSA_STORE_CURRENCY must be an uppercase ISO-4217 code" >&2; exit 2
fi
if [[ "$allow_writes" != 0 && "$allow_writes" != 1 ]]; then
  echo "MEDUSA_ALLOW_WRITES must be 0 or 1" >&2; exit 2
fi
if [[ "$keep_changes" != 0 && "$keep_changes" != 1 ]]; then
  echo "MEDUSA_KEEP_CHANGES must be 0 or 1" >&2; exit 2
fi

api="$base_url/admin"
tmp="$(mktemp -d)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
product_id=""
variant_id=""
inventory_id=""
location_id=""
changed_product=0
changed_price=0
changed_stock=0

request() {
  local name="$1" method="$2" path="$3" body="${4:-}" auth="${5:-yes}" status
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30
    --request "$method" --output "$tmp/$name.body" --write-out '%{http_code}'
    --header 'Accept: application/json')
  [[ "$insecure_tls" == 1 ]] && args+=(--insecure)
  [[ -n "$body" ]] && args+=(--header 'Content-Type: application/json' --data "$body")
  [[ "$auth" == yes ]] && args+=(--header "Authorization: Basic $token")
  status="$(curl "${args[@]}" "$api/$path")"
  printf '%s\n' "$status" > "$tmp/$name.status"
}
silent_post() {
  local path="$1" body="$2" status
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30
    --request POST --output "$tmp/cleanup.body" --write-out '%{http_code}'
    --header 'Accept: application/json' --header 'Content-Type: application/json'
    --header "Authorization: Basic $token" --data "$body")
  [[ "$insecure_tls" == 1 ]] && args+=(--insecure)
  status="$(curl "${args[@]}" "$api/$path" 2>/dev/null || true)"
  [[ "$status" == 2* ]]
}
status_of() { cat "$tmp/$1.status"; }
assert_status() {
  local name="$1" expected="$2" got
  got="$(status_of "$name")"
  [[ "$got" == "$expected" ]] || { echo "FAIL $name: expected HTTP $expected, got ${got:-unknown}" >&2; exit 1; }
  echo "PASS $name (HTTP $got)"
}
assert_denied() {
  local name="$1" got
  got="$(status_of "$name")"
  [[ "$got" == 401 || "$got" == 403 ]] || { echo "FAIL $name: expected HTTP 401 or 403, got ${got:-unknown}" >&2; exit 1; }
  echo "PASS $name (HTTP $got)"
}
assert_field() {
  local name="$1" field="$2"
  python3 - "$tmp/$name.body" "$field" <<'PY'
import json, sys
try:
    value = json.load(open(sys.argv[1], encoding="utf-8"))
    for part in sys.argv[2].split('.'):
        value = value[int(part)] if isinstance(value, list) else value[part]
except (OSError, ValueError, KeyError, IndexError, TypeError) as exc:
    raise SystemExit(f"missing or invalid JSON field {sys.argv[2]}: {exc}")
if value is None:
    raise SystemExit(f"missing JSON field {sys.argv[2]}")
PY
  echo "PASS $name (JSON $field present)"
}
cleanup() {
  local result=$?
  if [[ "$allow_writes" == 1 && "$keep_changes" == 0 ]]; then
    set +e
    if [[ "$changed_stock" == 1 && -s "$tmp/restore-stock.json" ]]; then
      silent_post "inventory-items/$inventory_id/location-levels/$location_id" "$(cat "$tmp/restore-stock.json")" || echo "WARN Medusa inventory cleanup failed" >&2
    fi
    if [[ "$changed_price" == 1 && -s "$tmp/restore-price.json" ]]; then
      silent_post "products/$product_id/variants/$variant_id" "$(cat "$tmp/restore-price.json")" || echo "WARN Medusa price cleanup failed" >&2
    fi
    if [[ "$changed_product" == 1 && -s "$tmp/restore-product.json" ]]; then
      silent_post "products/$product_id" "$(cat "$tmp/restore-product.json")" || echo "WARN Medusa product cleanup failed" >&2
    fi
    set -e
  fi
  rm -rf "$tmp"
  exit "$result"
}
trap cleanup EXIT

echo "Medusa v2 Admin REST smoke: $base_url"
request unauthorized GET 'products?offset=0&limit=1' '' no
assert_denied unauthorized
request products GET 'products?offset=0&limit=10&order=updated_at&fields=id,title,status,description,updated_at,variants.id,variants.sku,variants.prices.amount,variants.prices.currency_code'
assert_status products 200
assert_field products products
assert_field products count
python3 - "$tmp/products.body" "$sku" "$tmp/selection.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
items = value.get("products")
count = value.get("count")
if not isinstance(items, list) or not isinstance(count, int) or count < 0 or len(items) > 10:
    raise SystemExit("Medusa product list has invalid bounded shape")
for product in items:
    for variant in product.get("variants", []):
        if variant.get("sku") == sys.argv[2]:
            json.dump({"product_id": product.get("id"), "variant_id": variant.get("id")}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
            raise SystemExit(0)
raise SystemExit("MEDUSA_TEST_SKU was not found in the first bounded product page")
PY
product_id="$(python3 - "$tmp/selection.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
if not v.get("product_id") or not v.get("variant_id"): raise SystemExit("Medusa SKU mapping is incomplete")
print(v["product_id"])
PY
)"
variant_id="$(python3 - "$tmp/selection.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["variant_id"])
PY
)"
echo "PASS catalog (synthetic SKU mapped to product and variant)"

request product GET "products/$product_id?fields=id,title,status,description,updated_at,variants.id,variants.sku"
assert_status product 200
assert_field product product.id
assert_field product product.title
assert_field product product.status
assert_field product product.variants
python3 - "$tmp/product.body" "$tmp/product.json" "$product_id" "$sku" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8")).get("product", {})
if value.get("id") != sys.argv[3] or not value.get("title") or not value.get("status"):
    raise SystemExit("Medusa product identity/title/status is invalid")
if not any(v.get("id") and v.get("sku") == sys.argv[4] for v in value.get("variants", [])):
    raise SystemExit("Medusa product response does not contain the requested SKU")
json.dump(value, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
PY

request variant GET "products/$product_id/variants/$variant_id?fields=id,prices.amount,prices.currency_code"
assert_status variant 200
assert_field variant variant.id
assert_field variant variant.prices
python3 - "$tmp/variant.body" "$tmp/variant.json" "$variant_id" "$currency" "$tmp/restore-price.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8")).get("variant", {})
if v.get("id") != sys.argv[3]: raise SystemExit("Medusa variant identity is invalid")
wanted = None
for price in v.get("prices", []):
    if str(price.get("currency_code", "")).upper() == sys.argv[4]:
        wanted = price
        break
if wanted is None or wanted.get("amount") is None: raise SystemExit("Medusa variant has no price for MEDUSA_STORE_CURRENCY")
json.dump(v, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
try:
    # Medusa's amount schema is numeric. Keep the exact decimal text rather
    # than coercing through a binary float when preparing the restore body.
    amount = format(__import__("decimal").Decimal(str(wanted["amount"])), "f")
except Exception as exc:
    raise SystemExit(f"Medusa variant price is not a decimal: {exc}")
open(sys.argv[5], "w", encoding="utf-8").write(
    '{"prices":[{"currency_code":' + json.dumps(str(wanted["currency_code"]).lower()) + ',"amount":' + amount + '}]}'
)
PY
echo "PASS variant (currency=$currency)"

request inventory_item GET "inventory-items?sku=$sku&limit=1&fields=id,sku,location_levels.location_id,location_levels.stocked_quantity,location_levels.available_quantity"
assert_status inventory_item 200
assert_field inventory_item inventory_items
python3 - "$tmp/inventory_item.body" "$tmp/inventory.json" "$tmp/restore-stock.json" <<'PY'
import json, sys
page = json.load(open(sys.argv[1], encoding="utf-8"))
items = page.get("inventory_items", [])
if len(items) != 1 or not items[0].get("id"):
    raise SystemExit("Medusa inventory item for SKU is missing or ambiguous")
item = items[0]
levels = item.get("location_levels", [])
if not levels: raise SystemExit("Medusa inventory item has no location level")
level = levels[0]
if not level.get("location_id") or level.get("stocked_quantity") is None:
    raise SystemExit("Medusa inventory level is incomplete")
json.dump(item, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
json.dump({"stocked_quantity": level["stocked_quantity"]}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
PY
inventory_id="$(python3 - "$tmp/inventory.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["id"])
PY
)"
location_id="$(python3 - "$tmp/inventory.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["location_levels"][0]["location_id"])
PY
)"
echo "PASS inventory (item=$inventory_id location=$location_id)"

request locations GET 'stock-locations?limit=1&fields=id,name'
assert_status locations 200
assert_field locations stock_locations
python3 - "$tmp/locations.body" "$location_id" <<'PY'
import json, sys
locations = json.load(open(sys.argv[1], encoding="utf-8")).get("stock_locations", [])
if not locations or not any(row.get("id") == sys.argv[2] for row in locations):
    raise SystemExit("Medusa inventory location is not visible to the API token")
PY
echo "PASS locations (write location is visible)"

if [[ -n "$order_id" ]]; then
  request order GET "orders/$order_id?fields=id,display_id,status,created_at,updated_at"
  assert_status order 200
  assert_field order order.id
  assert_field order order.status
  request returns GET "returns?order_id=$order_id&offset=0&limit=5&fields=id,status,order_id,refund_amount,created_at"
  assert_status returns 200
  assert_field returns returns
  assert_field returns count
  echo "PASS order/returns reads (order=$order_id)"
fi

if [[ "$allow_writes" == 1 ]]; then
  python3 - "$tmp/product.json" "$tmp/restore-product.json" "$product_id" "$run_id" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
json.dump({"title": v["title"], "description": v.get("description") or "", "status": v["status"]}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
v["_smoke_title"] = f"TORGNEXA Medusa smoke {sys.argv[4]}"
v["_smoke_description"] = f"Synthetic TORGNEXA Medusa smoke {sys.argv[4]}"
json.dump(v, open(sys.argv[1], "w", encoding="utf-8"), separators=(",", ":"))
PY
  update="$(python3 - "$tmp/product.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
print(json.dumps({"title": v["_smoke_title"], "description": v["_smoke_description"], "status": v["status"]}, separators=(",", ":")))
PY
)"
  request product_update POST "products/$product_id" "$update"
  assert_status product_update 200
  changed_product=1
  request product_after GET "products/$product_id?fields=id,title,status,description,variants.id,variants.sku"
  assert_status product_after 200
  python3 - "$tmp/product_after.body" "$tmp/product.json" <<'PY'
import json, sys
a = json.load(open(sys.argv[1], encoding="utf-8")).get("product", {}); e = json.load(open(sys.argv[2], encoding="utf-8"))
if a.get("title") != e.get("_smoke_title") or a.get("description") != e.get("_smoke_description"):
    raise SystemExit("Medusa product update was not reconciled")
PY
  echo "PASS product_update (read-after-write reconciled)"

  new_price="$(python3 - "$tmp/variant.json" "$currency" <<'PY'
import json, sys
from decimal import Decimal
v = json.load(open(sys.argv[1], encoding="utf-8"))
p = next(x["amount"] for x in v["prices"] if str(x.get("currency_code", "")).upper() == sys.argv[2])
print(format(Decimal(str(p)) + Decimal("0.01"), "f"))
PY
)"
price_body="$(python3 - "$currency" "$new_price" <<'PY'
import json, sys
print('{"prices":[{"currency_code":' + json.dumps(sys.argv[1].lower()) + ',"amount":' + sys.argv[2] + '}]}')
PY
)"
  request price_update POST "products/$product_id/variants/$variant_id" "$price_body"
  assert_status price_update 200
  changed_price=1
  request variant_after GET "products/$product_id/variants/$variant_id?fields=id,prices.amount,prices.currency_code"
  assert_status variant_after 200
  python3 - "$tmp/variant_after.body" "$currency" "$new_price" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8")).get("variant", {})
for p in v.get("prices", []):
    if str(p.get("currency_code", "")).upper() == sys.argv[2] and str(p.get("amount")) == sys.argv[3]:
        break
else: raise SystemExit("Medusa price was not reconciled")
PY
  echo "PASS price_update (read-after-write reconciled)"

  new_qty="$(python3 - "$tmp/inventory.json" <<'PY'
import json, sys
from decimal import Decimal
q = json.load(open(sys.argv[1], encoding="utf-8"))["location_levels"][0]["stocked_quantity"]
if isinstance(q, bool): raise SystemExit("invalid Medusa stock quantity")
try: print(format(Decimal(str(q)) + Decimal("1"), "f"))
except Exception as exc: raise SystemExit(f"invalid Medusa stock quantity: {exc}")
PY
)"
stock_body="$(python3 - "$new_qty" <<'PY'
import sys
print('{"stocked_quantity":' + sys.argv[1] + '}')
PY
)"
  request inventory_update POST "inventory-items/$inventory_id/location-levels/$location_id" "$stock_body"
  assert_status inventory_update 200
  changed_stock=1
  request inventory_after GET "inventory-items?sku=$sku&limit=1&fields=id,sku,location_levels.location_id,location_levels.stocked_quantity,location_levels.available_quantity"
  assert_status inventory_after 200
  python3 - "$tmp/inventory_after.body" "$location_id" "$new_qty" <<'PY'
import json, sys
items = json.load(open(sys.argv[1], encoding="utf-8")).get("inventory_items", [])
for item in items:
    for level in item.get("location_levels", []):
        if level.get("location_id") == sys.argv[2] and str(level.get("stocked_quantity")) == sys.argv[3]:
            raise SystemExit(0)
raise SystemExit("Medusa inventory quantity was not reconciled")
PY
  echo "PASS inventory_update (read-after-write reconciled)"
else
  echo "INFO writes skipped (set MEDUSA_ALLOW_WRITES=1 on a disposable non-production store)"
fi

echo "Medusa v2 Admin REST smoke: all checks passed"
