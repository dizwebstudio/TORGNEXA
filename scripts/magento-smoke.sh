#!/usr/bin/env bash
set -euo pipefail

# Credentialed Magento 2 / Adobe Commerce REST smoke test. It is separate from
# the deterministic Connector SDK conformance suite and never prints response
# bodies or credentials.
base_url="${MAGENTO_BASE_URL:-}"
token="${MAGENTO_TOKEN:-}"
sku="${MAGENTO_TEST_SKU:-}"
order_id="${MAGENTO_TEST_ORDER_ID:-}"
allow_http="${MAGENTO_ALLOW_HTTP:-0}"
insecure_tls="${MAGENTO_INSECURE_TLS:-0}"
allow_writes="${MAGENTO_ALLOW_WRITES:-0}"
keep_changes="${MAGENTO_KEEP_CHANGES:-0}"

if [[ -z "$base_url" || -z "$token" || -z "$sku" ]]; then
  cat >&2 <<'EOF'
Magento live smoke requires MAGENTO_BASE_URL, MAGENTO_TOKEN and MAGENTO_TEST_SKU.
Use a dedicated non-production Integration token and an existing synthetic SKU;
product creation is intentionally unsupported by the connector.
EOF
  exit 2
fi
base_url="${base_url%/}"
python3 - "$base_url" "$allow_http" <<'PY'
from urllib.parse import urlsplit
import sys
u = urlsplit(sys.argv[1])
if u.scheme not in {"http", "https"} or not u.netloc:
    raise SystemExit("MAGENTO_BASE_URL must be an absolute HTTP(S) URL")
if u.username or u.password or u.query or u.fragment:
    raise SystemExit("MAGENTO_BASE_URL must not contain credentials, query or fragment")
if u.path.rstrip("/").endswith("/rest/V1"):
    raise SystemExit("MAGENTO_BASE_URL must be the store root, without /rest/V1")
if u.scheme != "https" and sys.argv[2] != "1":
    raise SystemExit("HTTPS is required; set MAGENTO_ALLOW_HTTP=1 only for an isolated local store")
if u.scheme == "http" and u.hostname not in {"127.0.0.1", "localhost", "::1"}:
    raise SystemExit("plain HTTP is restricted to loopback; use HTTPS for a remote store")
PY
if [[ ${#token} -lt 8 || ${#token} -gt 4096 || "$token" != "${token//[$'\n\r\t ']/}" ]]; then
  echo "MAGENTO_TOKEN must be a non-whitespace token between 8 and 4096 characters" >&2; exit 2
fi
if [[ ! "$sku" =~ ^[A-Za-z0-9._-]{1,200}$ ]]; then
  echo "MAGENTO_TEST_SKU must contain only ASCII letters, digits, dot, underscore or hyphen" >&2; exit 2
fi
if [[ -n "$order_id" && ! "$order_id" =~ ^[1-9][0-9]{0,18}$ ]]; then
  echo "MAGENTO_TEST_ORDER_ID must be a positive numeric entity_id" >&2; exit 2
fi
if [[ "$allow_writes" != 0 && "$allow_writes" != 1 ]]; then
  echo "MAGENTO_ALLOW_WRITES must be 0 or 1" >&2; exit 2
fi
if [[ "$keep_changes" != 0 && "$keep_changes" != 1 ]]; then
  echo "MAGENTO_KEEP_CHANGES must be 0 or 1" >&2; exit 2
fi

api="$base_url/rest/V1"
tmp="$(mktemp -d)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
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
  [[ "$auth" == yes ]] && args+=(--header "Authorization: Bearer $token")
  status="$(curl "${args[@]}" "$api/$path")"
  printf '%s\n' "$status" > "$tmp/$name.status"
}

silent_put() {
  local path="$1" body="$2" status
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30
    --request PUT --output "$tmp/cleanup.body" --write-out '%{http_code}'
    --header 'Accept: application/json' --header 'Content-Type: application/json'
    --header "Authorization: Bearer $token" --data "$body")
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
      silent_put "products/$sku/stockItems/$(cat "$tmp/item-id")" "$(cat "$tmp/restore-stock.json")" || echo "WARN Magento inventory cleanup failed" >&2
    fi
    if [[ "$changed_price" == 1 && -s "$tmp/restore-price.json" ]]; then
      silent_put "products/$sku" "$(cat "$tmp/restore-price.json")" || echo "WARN Magento price cleanup failed" >&2
    fi
    if [[ "$changed_product" == 1 && -s "$tmp/restore-product.json" ]]; then
      silent_put "products/$sku" "$(cat "$tmp/restore-product.json")" || echo "WARN Magento product cleanup failed" >&2
    fi
    set -e
  fi
  rm -rf "$tmp"
  exit "$result"
}
trap cleanup EXIT

echo "Magento / Adobe Commerce REST smoke: $base_url"
request unauthorized GET 'products?searchCriteria[currentPage]=1&searchCriteria[pageSize]=1' '' no
assert_denied unauthorized
request products GET 'products?searchCriteria[currentPage]=1&searchCriteria[pageSize]=5&searchCriteria[sortOrders][0][field]=entity_id&searchCriteria[sortOrders][0][direction]=ASC'
assert_status products 200
assert_field products items
assert_field products total_count

encoded_sku="$(python3 - "$sku" <<'PY'
from urllib.parse import quote
import sys
print(quote(sys.argv[1], safe=""))
PY
)"
request product GET "products/$encoded_sku"
assert_status product 200
for field in sku name status price; do assert_field product "$field"; done
python3 - "$tmp/product.body" "$tmp/product.json" "$sku" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if value.get("sku") != sys.argv[3] or not isinstance(value.get("name"), str) or not value["name"].strip():
    raise SystemExit("Magento product SKU/name is invalid")
json.dump(value, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
PY

request stock GET "stockItems/$encoded_sku"
assert_status stock 200
for field in item_id qty is_in_stock; do assert_field stock "$field"; done
python3 - "$tmp/stock.body" "$tmp/item-id" "$tmp/restore-stock.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if not isinstance(value.get("item_id"), int) or value["item_id"] < 1:
    raise SystemExit("Magento stock response has no positive item_id")
if isinstance(value.get("qty"), bool) or not isinstance(value.get("qty"), (int, float)) or value["qty"] < 0:
    raise SystemExit("Magento stock response has invalid qty")
if not isinstance(value.get("is_in_stock"), bool):
    raise SystemExit("Magento stock response has invalid is_in_stock")
open(sys.argv[2], "w", encoding="utf-8").write(str(value["item_id"]))
json.dump({"stockItem": {"qty": value["qty"], "is_in_stock": value["is_in_stock"]}}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
PY
echo "PASS stock (item_id=$(cat "$tmp/item-id"))"

if [[ -n "$order_id" ]]; then
  request order GET "orders/$order_id"
  assert_status order 200
  assert_field order entity_id
  assert_field order status
  request returns GET "creditmemos?searchCriteria[currentPage]=1&searchCriteria[pageSize]=5&searchCriteria[filter_groups][0][filters][0][field]=order_id&searchCriteria[filter_groups][0][filters][0][value]=$order_id&searchCriteria[filter_groups][0][filters][0][condition_type]=eq"
  assert_status returns 200
  assert_field returns items
  assert_field returns total_count
  echo "PASS order/returns reads (entity_id=$order_id)"
fi

if [[ "$allow_writes" == 1 ]]; then
  python3 - "$tmp/product.json" "$tmp/restore-product.json" "$sku" "$run_id" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
description = next((a.get("value", "") for a in value.get("custom_attributes", []) if a.get("attribute_code") == "description"), "")
json.dump({"product": {"sku": sys.argv[3], "name": value["name"], "status": value["status"], "custom_attributes": [{"attribute_code": "description", "value": description}]}}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
value["_smoke_name"] = f"TORGNEXA Magento smoke {sys.argv[4]}"
value["_smoke_description"] = f"Synthetic TORGNEXA Magento smoke {sys.argv[4]}"
json.dump(value, open(sys.argv[1], "w", encoding="utf-8"), separators=(",", ":"))
PY
  update="$(python3 - "$tmp/product.json" "$sku" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
print(json.dumps({"product": {"sku": sys.argv[2], "name": v["_smoke_name"], "status": v["status"], "custom_attributes": [{"attribute_code": "description", "value": v["_smoke_description"]}]}}, separators=(",", ":")))
PY
)"
  request product_update PUT "$encoded_sku" "$update"
  assert_status product_update 200
  changed_product=1
  request product_after GET "products/$encoded_sku"
  assert_status product_after 200
  python3 - "$tmp/product_after.body" "$tmp/product.json" <<'PY'
import json, sys
a = json.load(open(sys.argv[1], encoding="utf-8")); e = json.load(open(sys.argv[2], encoding="utf-8"))
if a.get("name") != e.get("_smoke_name"):
    raise SystemExit("Magento product name was not reconciled")
d = next((x.get("value") for x in a.get("custom_attributes", []) if x.get("attribute_code") == "description"), "")
if d != e.get("_smoke_description"):
    raise SystemExit("Magento product description was not reconciled")
PY
  echo "PASS product_update (read-after-write reconciled)"

  new_price="$(python3 - "$tmp/product.json" "$tmp/restore-price.json" "$sku" <<'PY'
import json, re, sys
from decimal import Decimal
raw = open(sys.argv[1], encoding="utf-8").read()
m = re.search(r'"price"\s*:\s*(-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?)', raw)
if not m: raise SystemExit("Magento product has no numeric price")
p = m.group(1)
open(sys.argv[2], "w", encoding="utf-8").write('{"product":{"sku":' + json.dumps(sys.argv[3]) + ',"price":' + p + '}}')
print(format(Decimal(p) + Decimal("0.01"), "f"))
PY
)"
  price_body="$(python3 - "$sku" "$new_price" <<'PY'
import json, sys
print('{"product":{"sku":' + json.dumps(sys.argv[1]) + ',"price":' + sys.argv[2] + '}}')
PY
)"
  request price_update PUT "$encoded_sku" "$price_body"
  assert_status price_update 200
  changed_price=1
  request price_after GET "products/$encoded_sku"
  assert_status price_after 200
  python3 - "$tmp/price_after.body" "$new_price" <<'PY'
import json, sys
if str(json.load(open(sys.argv[1], encoding="utf-8")).get("price")) != sys.argv[2]:
    raise SystemExit("Magento price was not reconciled")
PY
  echo "PASS price_update (read-after-write reconciled)"

  new_qty="$(python3 - "$tmp/stock.body" "$tmp/restore-stock.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8")); q = v["qty"]
if isinstance(q, bool) or not isinstance(q, int): raise SystemExit("Magento smoke writes require an integer stock qty")
json.dump({"stockItem": {"qty": q, "is_in_stock": v["is_in_stock"]}}, open(sys.argv[2], "w", encoding="utf-8"), separators=(",", ":"))
print(q + 1)
PY
)"
  stock_body="$(python3 - "$new_qty" <<'PY'
import json, sys
print(json.dumps({"stockItem": {"qty": int(sys.argv[1]), "is_in_stock": int(sys.argv[1]) > 0}}, separators=(",", ":")))
PY
)"
  request inventory_update PUT "$encoded_sku/stockItems/$(cat "$tmp/item-id")" "$stock_body"
  assert_status inventory_update 200
  changed_stock=1
  request inventory_after GET "stockItems/$encoded_sku"
  assert_status inventory_after 200
  python3 - "$tmp/inventory_after.body" "$new_qty" <<'PY'
import json, sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("qty") != int(sys.argv[2]):
    raise SystemExit("Magento inventory qty was not reconciled")
PY
  echo "PASS inventory_update (read-after-write reconciled)"
else
  echo "INFO writes skipped (set MAGENTO_ALLOW_WRITES=1 on a disposable non-production store)"
fi

echo "Magento / Adobe Commerce REST smoke: all checks passed"
