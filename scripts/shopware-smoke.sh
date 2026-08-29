#!/usr/bin/env bash
set -euo pipefail

# Credentialed Shopware Admin API smoke. The target must be an isolated
# non-production store. The script never prints credentials or response bodies;
# temporary response files are removed by the EXIT trap.
base_url="${SHOPWARE_BASE_URL:-http://127.0.0.1:18005}"
client_id="${SHOPWARE_CLIENT_ID:-}"
client_secret="${SHOPWARE_CLIENT_SECRET:-}"
sku="${SHOPWARE_TEST_SKU:-SWDEMO10002}"
currency="${SHOPWARE_STORE_CURRENCY:-EUR}"
host_header="${SHOPWARE_HOST_HEADER:-localhost}"
allow_http="${SHOPWARE_ALLOW_HTTP:-0}"
insecure_tls="${SHOPWARE_INSECURE_TLS:-0}"
allow_writes="${SHOPWARE_ALLOW_WRITES:-0}"
keep_changes="${SHOPWARE_KEEP_CHANGES:-0}"

if [[ -z "$client_id" || -z "$client_secret" ]]; then
  cat >&2 <<'EOF'
Shopware smoke requires SHOPWARE_CLIENT_ID and SHOPWARE_CLIENT_SECRET.
Create a temporary Settings > System > Integrations credential (or run
`php bin/console integration:create --admin smoke-torgnexa`) in a disposable
Shopware store. Never commit or print the generated secret.
EOF
  exit 2
fi
base_url="${base_url%/}"
python3 - "$base_url" "$allow_http" <<'PY'
from urllib.parse import urlsplit
import sys
u = urlsplit(sys.argv[1])
if u.scheme not in {"http", "https"} or not u.netloc:
    raise SystemExit("SHOPWARE_BASE_URL must be an absolute HTTP(S) URL")
if u.username or u.password or u.query or u.fragment:
    raise SystemExit("SHOPWARE_BASE_URL must not contain credentials, query or fragment")
if u.path.rstrip("/").endswith("/api"):
    raise SystemExit("SHOPWARE_BASE_URL must be the Shopware root, without /api")
if u.scheme != "https" and sys.argv[2] != "1":
    raise SystemExit("HTTPS is required; set SHOPWARE_ALLOW_HTTP=1 only for loopback disposable Docker")
if u.scheme == "http" and u.hostname not in {"127.0.0.1", "localhost", "::1"}:
    raise SystemExit("plain HTTP is restricted to loopback; use HTTPS for a remote store")
PY
for value_name in client_id client_secret; do
  value="${!value_name}"
  if [[ ${#value} -lt 8 || ${#value} -gt 4096 || "$value" != "${value//[$'\n\r\t ']/}" ]]; then
    echo "SHOPWARE_${value_name^^} must be a non-whitespace value between 8 and 4096 characters" >&2
    exit 2
  fi
done
if [[ ! "$sku" =~ ^[A-Za-z0-9._-]{1,200}$ ]]; then
  echo "SHOPWARE_TEST_SKU must contain only ASCII letters, digits, dot, underscore or hyphen" >&2
  exit 2
fi
if [[ ! "$currency" =~ ^[A-Z]{3}$ ]]; then
  echo "SHOPWARE_STORE_CURRENCY must be an uppercase ISO-4217 code" >&2
  exit 2
fi
if [[ "$allow_writes" != 0 && "$allow_writes" != 1 ]]; then
  echo "SHOPWARE_ALLOW_WRITES must be 0 or 1" >&2
  exit 2
fi
if [[ "$keep_changes" != 0 && "$keep_changes" != 1 ]]; then
  echo "SHOPWARE_KEEP_CHANGES must be 0 or 1" >&2
  exit 2
fi

api="$base_url/api"
tmp="$(mktemp -d)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
token=""
product_id=""
original_product=""
original_price=""
original_stock=""
changed_product=0
changed_price=0
changed_stock=0

cleanup() {
  local result=$?
  if [[ "$allow_writes" == 1 && "$keep_changes" == 0 && -n "$token" && -n "$product_id" ]]; then
    set +e
    [[ "$changed_stock" == 1 && -s "$tmp/restore-stock.json" ]] && request restore_stock PATCH "product/$product_id" "$(cat "$tmp/restore-stock.json")" yes >/dev/null
    [[ "$changed_price" == 1 && -s "$tmp/restore-price.json" ]] && request restore_price PATCH "product/$product_id" "$(cat "$tmp/restore-price.json")" yes >/dev/null
    [[ "$changed_product" == 1 && -s "$tmp/restore-product.json" ]] && request restore_product PATCH "product/$product_id" "$(cat "$tmp/restore-product.json")" yes >/dev/null
    set -e
  fi
  rm -rf "$tmp"
  exit "$result"
}
trap cleanup EXIT

request() {
  local name="$1" method="$2" path="$3" body="${4:-}" auth="${5:-yes}" status
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30
    --request "$method" --output "$tmp/$name.body" --write-out '%{http_code}'
    --header 'Accept: application/json')
  [[ "$insecure_tls" == 1 ]] && args+=(--insecure)
  [[ -n "$host_header" ]] && args+=(--header "Host: $host_header")
  [[ -n "$body" ]] && args+=(--header 'Content-Type: application/json' --data "$body")
  [[ "$auth" == yes ]] && args+=(--header "Authorization: Bearer $token")
  status="$(curl "${args[@]}" "$api/$path" || true)"
  printf '%s\n' "$status" > "$tmp/$name.status"
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
  [[ "$got" == 401 || "$got" == 403 ]] || { echo "FAIL $name: expected HTTP 401/403, got ${got:-unknown}" >&2; exit 1; }
  echo "PASS $name (HTTP $got)"
}
json_check() {
  local name="$1" expression="$2"
  python3 - "$tmp/$name.body" "$expression" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if value.get("errors"):
    raise SystemExit("Shopware returned an API error")
safe = {"v": value, "isinstance": isinstance, "list": list, "dict": dict, "any": any, "len": len, "str": str}
if not eval(sys.argv[2], {"__builtins__": {}}, safe):
    raise SystemExit("Shopware response shape check failed")
PY
}
assert_product_field() {
  local name="$1" field="$2" expected="$3"
  python3 - "$tmp/$name.body" "$field" "$expected" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
row = value.get("data", {})
attrs = row.get("attributes", {}) if isinstance(row, dict) else {}
if not attrs and isinstance(row, dict):
    attrs = row
actual = attrs.get(sys.argv[2], row.get(sys.argv[2]))
if str(actual) != sys.argv[3]:
    raise SystemExit(f"Shopware {sys.argv[2]} mismatch: expected {sys.argv[3]!r}")
PY
}

echo "Shopware 6 Admin API smoke: $base_url"
request token POST oauth/token "{\"grant_type\":\"client_credentials\",\"client_id\":$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$client_id"),\"client_secret\":$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$client_secret")}" no
assert_status token 200
token="$(python3 - "$tmp/token.body" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if not isinstance(value.get("access_token"), str) or not value["access_token"] or not isinstance(value.get("expires_in"), int):
    raise SystemExit("Shopware token response is invalid")
print(value["access_token"])
PY
)"
echo "PASS oauth (client_credentials token received)"

request unauthorized GET product '' no
assert_denied unauthorized

request products POST search/product '{"page":1,"limit":50,"sort":[{"field":"updatedAt","order":"ASC"}],"filter":[{"type":"equals","field":"parentId","value":null}]}' yes
assert_status products 200
product_id="$(python3 - "$tmp/products.body" "$sku" "$tmp/selection.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
rows = value.get("data")
if not isinstance(rows, list) or len(rows) > 50:
    raise SystemExit("Shopware product page is not bounded")
for row in rows:
    attrs = row.get("attributes", {}) if isinstance(row, dict) else {}
    sku = attrs.get("productNumber", row.get("productNumber"))
    if sku == sys.argv[2]:
        pid = row.get("id", attrs.get("id"))
        if not isinstance(pid, str) or not pid:
            raise SystemExit("Shopware product id is missing")
        json.dump({"id": pid, "attrs": attrs}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
        print(pid)
        raise SystemExit(0)
raise SystemExit("SHOPWARE_TEST_SKU was not found in the bounded product page")
PY
)"
echo "PASS catalog (SKU mapped to product $product_id)"

request product GET "product/$product_id"
assert_status product 200
python3 - "$tmp/product.body" "$tmp/product.json" "$product_id" "$sku" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
row = value.get("data", {})
attrs = row.get("attributes", {}) if isinstance(row, dict) else {}
if not attrs and isinstance(row, dict):
    attrs = row
if row.get("id") != sys.argv[3] or attrs.get("productNumber", row.get("productNumber")) != sys.argv[4]:
    raise SystemExit("Shopware product identity/SKU is invalid")
if not attrs.get("name", row.get("name")) or not attrs.get("updatedAt", row.get("updatedAt")):
    raise SystemExit("Shopware product detail is incomplete")
json.dump({"productNumber": attrs.get("productNumber", row.get("productNumber")), "name": attrs.get("name", row.get("name")), "description": attrs.get("description", row.get("description")), "active": attrs.get("active", row.get("active")), "stock": attrs.get("stock", row.get("stock")), "price": attrs.get("price", row.get("price"))}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
PY
echo "PASS product (JSON:API detail/attributes mapped)"

request currencies POST search/currency '{"page":1,"limit":500}'
assert_status currencies 200
currency_id="$(python3 - "$tmp/currencies.body" "$currency" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
for row in value.get("data", []):
    attrs = row.get("attributes", {})
    if str(attrs.get("isoCode", row.get("isoCode", ""))).upper() == sys.argv[2]:
        print(row.get("id", attrs.get("id", "")))
        raise SystemExit(0)
raise SystemExit("SHOPWARE_STORE_CURRENCY is not configured in the store")
PY
)"
[[ -n "$currency_id" ]] || { echo "FAIL currency: id missing" >&2; exit 1; }
echo "PASS currency ($currency)"

python3 - "$tmp/product.json" "$currency_id" "$tmp/restore-price.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
prices = v.get("price") or []
for price in prices:
    if price.get("currencyId") == sys.argv[2]:
        json.dump({"price": [price]}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
        raise SystemExit(0)
raise SystemExit("selected Shopware product has no price for SHOPWARE_STORE_CURRENCY")
PY
echo "PASS price (currency price mapped)"

python3 - "$tmp/product.json" "$tmp/restore-stock.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
stock = v.get("stock")
if not isinstance(stock, int) or stock < 0:
    raise SystemExit("Shopware product stock is not a non-negative integer")
json.dump({"stock": stock}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
PY
echo "PASS inventory (stock mapped)"

request orders POST search/order '{"page":1,"limit":1,"associations":{"stateMachineState":{},"lineItems":{}}}'
assert_status orders 200
json_check orders 'isinstance(v.get("data"), list) and len(v["data"]) <= 1'
echo "PASS orders (bounded search endpoint)"
request refunds POST search/order-transaction-capture-refund '{"page":1,"limit":1}'
assert_status refunds 200
json_check refunds 'isinstance(v.get("data"), list) and len(v["data"]) <= 1'
echo "PASS refunds (hyphenated entity endpoint)"

if [[ "$allow_writes" == 1 ]]; then
  tmp_name="TORGNEXA Shopware Smoke $run_id"
  tmp_description="TORGNEXA disposable Shopware smoke $run_id"
  python3 - "$tmp/product.json" "$tmp/restore-product.json" "$sku" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
json.dump({"productNumber": v["productNumber"], "name": v["name"], "description": v.get("description"), "active": v.get("active")}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
PY
  request write_product PATCH "product/$product_id" "$(python3 - "$sku" "$tmp_name" "$tmp_description" <<'PY'
import json,sys
print(json.dumps({"productNumber":sys.argv[1],"name":sys.argv[2],"description":sys.argv[3],"active":True},separators=(",",":")))
PY
)"
  assert_status write_product 204
  changed_product=1
  request product_after_product GET "product/$product_id"
  assert_status product_after_product 200
  assert_product_field product_after_product name "$tmp_name"
  echo "PASS product write/read-after-write"

  new_price="$(python3 - "$tmp/product.json" "$currency_id" <<'PY'
import json,sys
from decimal import Decimal
v=json.load(open(sys.argv[1]))
for p in v.get("price") or []:
    if p.get("currencyId") == sys.argv[2]:
        print(format(Decimal(str(p["gross"])) + Decimal("1.23"), "f")); raise SystemExit
raise SystemExit("price missing")
PY
)"
  request write_price PATCH "product/$product_id" "$(python3 - "$currency_id" "$new_price" <<'PY'
import json,sys
v=__import__('decimal').Decimal(sys.argv[2])
print(json.dumps({"price":[{"currencyId":sys.argv[1],"gross":float(v),"net":float(v),"linked":False}]},separators=(",",":")))
PY
)"
  assert_status write_price 204
  changed_price=1
  request product_after_price GET "product/$product_id"
  assert_status product_after_price 200
  python3 - "$tmp/product_after_price.body" "$currency_id" "$new_price" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
row = value.get("data", {})
attrs = row.get("attributes", {}) if isinstance(row, dict) else {}
if not attrs and isinstance(row, dict):
    attrs = row
prices = attrs.get("price", []) or []
from decimal import Decimal
if not any(p.get("currencyId") == sys.argv[2] and Decimal(str(p.get("gross"))) == Decimal(sys.argv[3]) for p in prices):
    attrs = value.get("data", {}).get("attributes", {})
    raise SystemExit(f"Shopware price read-after-write mismatch (expected {sys.argv[3]}, got {prices!r}, attribute_keys={sorted(attrs)})")
PY
  echo "PASS price write/read-after-write"

  original_stock="$(python3 -c 'import json; print(json.load(open("'"$tmp/product.json"'"))["stock"])')"
  new_stock=$((original_stock + 1))
  request write_stock PATCH "product/$product_id" "{\"stock\":$new_stock}"
  assert_status write_stock 204
  changed_stock=1
  request product_after_stock GET "product/$product_id"
  assert_status product_after_stock 200
  assert_product_field product_after_stock stock "$new_stock"
  echo "PASS inventory write/read-after-write"
  echo "PASS cleanup (original product, price and stock restored on exit)"
else
  echo "INFO writes skipped (set SHOPWARE_ALLOW_WRITES=1 only for disposable data)"
fi

echo "Shopware 6 Admin API smoke: all checks passed"
