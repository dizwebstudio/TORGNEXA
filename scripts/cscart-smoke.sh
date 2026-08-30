#!/usr/bin/env bash
set -euo pipefail

# Credentialed CS-Cart API 2.0 smoke test. It targets an operator-provided
# non-production store and is deliberately separate from SDK conformance.
base_url="${CS_CART_BASE_URL:-}"
email="${CS_CART_EMAIL:-}"
api_key="${CS_CART_API_KEY:-}"
allow_http="${CS_CART_ALLOW_HTTP:-0}"
insecure_tls="${CS_CART_INSECURE_TLS:-0}"
keep_product="${CS_CART_KEEP_PRODUCT:-0}"

if [[ -z "$base_url" || -z "$email" || -z "$api_key" ]]; then
  cat >&2 <<'EOF'
CS-Cart live smoke requires CS_CART_BASE_URL, CS_CART_EMAIL and CS_CART_API_KEY.
Use a dedicated non-production administrator/API key; credentials are never
included in output.
EOF
  exit 2
fi
base_url="${base_url%/}"
python3 - "$base_url" "$allow_http" <<'PY'
from urllib.parse import urlsplit
import sys
url = urlsplit(sys.argv[1])
allow_http = sys.argv[2] == "1"
if url.scheme not in {"http", "https"} or not url.netloc:
    raise SystemExit("CS_CART_BASE_URL must be an absolute HTTP(S) URL")
if url.username or url.password or url.query or url.fragment:
    raise SystemExit("CS_CART_BASE_URL must not contain credentials, query or fragment")
if url.scheme != "https" and not allow_http:
    raise SystemExit("HTTPS is required; set CS_CART_ALLOW_HTTP=1 only for an isolated local store")
if url.scheme == "http" and url.hostname not in {"127.0.0.1", "localhost", "::1"}:
    raise SystemExit("plain HTTP is restricted to loopback; use HTTPS for a remote store")
PY
if [[ ! "$email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]; then
  echo "CS_CART_EMAIL is not a valid administrator e-mail" >&2; exit 2
fi
if [[ ${#api_key} -lt 16 ]]; then
  echo "CS_CART_API_KEY is too short for a CS-Cart API key" >&2; exit 2
fi

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
sku="${CS_CART_TEST_SKU:-TORGNEXA-CSCART-${run_id}}"
title="${CS_CART_TEST_TITLE:-TORGNEXA CS-Cart smoke ${run_id}}"
if [[ ! "$sku" =~ ^[A-Za-z0-9._-]{1,64}$ ]]; then
  echo "CS_CART_TEST_SKU contains unsupported characters" >&2; exit 2
fi
tmp_dir="$(mktemp -d)"
created_id=""
api_base="$base_url/api/2.0"

curl_args() {
  local -a args=(--silent --show-error --connect-timeout 5 --max-time 30)
  [[ "$insecure_tls" == "1" ]] && args+=(--insecure)
  args+=(--user "$email:$api_key")
  curl "${args[@]}" "$@"
}
cleanup() {
  local status
  [[ -z "$created_id" || "$keep_product" == "1" ]] && return
  set +e
  status="$(curl_args --request DELETE --output /dev/null --write-out '%{http_code}' "$api_base/products/$created_id")"
  set -e
  [[ "$status" == 2* ]] || echo "WARN cleanup failed (HTTP ${status:-unknown}); remove $sku manually" >&2
}
trap 'cleanup; rm -rf "$tmp_dir"' EXIT

request() {
  local name="$1" method="$2" path="$3" body="${4:-}" auth="${5:-yes}"
  local output="$tmp_dir/$name.body" status
  local -a args=(--request "$method" --output "$output" --write-out '%{http_code}')
  [[ -n "$body" ]] && args+=(--header 'Content-Type: application/json' --data "$body")
  if [[ "$auth" == "yes" ]]; then
    status="$(curl_args "${args[@]}" "$api_base/$path")"
  else
    local -a no_auth=(--silent --show-error --connect-timeout 5 --max-time 30)
    [[ "$insecure_tls" == "1" ]] && no_auth+=(--insecure)
    no_auth+=(--request "$method" --output "$output" --write-out '%{http_code}')
    [[ -n "$body" ]] && no_auth+=(--header 'Content-Type: application/json' --data "$body")
    status="$(curl "${no_auth[@]}" "$api_base/$path")"
  fi
  printf '%s\n' "$status" > "$tmp_dir/$name.status"
}
status_of() { cat "$tmp_dir/$1.status"; }
assert_status() {
  local name="$1" expected="$2" actual="$(status_of "$1")"
  [[ "$actual" == "$expected" ]] || { echo "FAIL $name: expected HTTP $expected, got ${actual:-unknown}" >&2; exit 1; }
  echo "PASS $name (HTTP $actual)"
}
json_value() {
  python3 - "$tmp_dir/$1.body" "$2" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
for part in sys.argv[2].split('.'):
    value = value[int(part)] if isinstance(value, list) else value[part]
print(value)
PY
}
assert_json() {
  local name="$1" expression="$2"
  python3 - "$tmp_dir/$name.body" "$expression" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
for part in sys.argv[2].split('.'):
    value = value[int(part)] if isinstance(value, list) else value[part]
if value is None:
    raise SystemExit("missing JSON field: " + sys.argv[2])
PY
  echo "PASS $name (JSON $expression present)"
}
create_body="$(python3 - "$sku" "$title" <<'PY'
import json, sys
print(json.dumps({"product":sys.argv[2],"product_code":sys.argv[1],"full_description":"Synthetic TORGNEXA CS-Cart live qualification product","status":"A"}, separators=(",", ":")))
PY
)"
update_body="$(python3 - "$sku" "$title" <<'PY'
import json, sys
print(json.dumps({"product":sys.argv[2]+" updated","product_code":sys.argv[1],"full_description":"Synthetic TORGNEXA CS-Cart live qualification product updated","status":"A"}, separators=(",", ":")))
PY
)"

echo "CS-Cart REST API 2.0 live smoke: $base_url"
request unauthorized GET 'products?items_per_page=1&pshort=Y&pfull=Y' '' no
assert_status unauthorized 401
request products GET 'products?page=1&items_per_page=1&pshort=Y&pfull=Y'
assert_status products 200
assert_json products products
request prices GET 'products?page=1&items_per_page=1&pshort=Y&pfull=Y'
assert_status prices 200
assert_json prices products.0.price
echo "PASS prices (base price projection present)"
request orders GET 'orders?page=1&items_per_page=1&sort_by=date&sort_order=asc'
assert_status orders 200
assert_json orders orders
echo "PASS orders (bounded order list projection present)"
request create POST products "$create_body"
actual="$(status_of create)"
[[ "$actual" == "200" || "$actual" == "201" ]] || { echo "FAIL create: expected HTTP 200 or 201, got ${actual:-unknown}" >&2; exit 1; }
created_id="$(json_value create product_id)"
[[ "$created_id" =~ ^[1-9][0-9]*$ ]] || { echo "FAIL create: response did not contain numeric product_id" >&2; exit 1; }
echo "PASS create (HTTP $actual, product_id=$created_id)"
request lookup GET "products?pcode=$sku&items_per_page=2&pshort=Y&pfull=Y"
assert_status lookup 200
python3 - "$tmp_dir/lookup.body" <<'PY'
import json, sys
products = json.load(open(sys.argv[1], encoding="utf-8")).get("products", [])
if len(products) != 1:
    raise SystemExit(f"expected one product for SKU, got {len(products)}")
PY
echo "PASS lookup (one product for SKU)"
request product GET "products/$created_id"
assert_status product 200
assert_json product product_code
assert_json product amount
[[ "$(json_value product product_code)" == "$sku" ]] || { echo "FAIL product: product_code mismatch" >&2; exit 1; }
echo "PASS product (product_code=$sku)"
request update PUT "products/$created_id" "$update_body"
assert_status update 200
request read_after_write GET "products/$created_id"
assert_status read_after_write 200
[[ "$(json_value read_after_write product)" == "$title updated" ]] || { echo "FAIL read_after_write: title mismatch" >&2; exit 1; }
echo "PASS read_after_write (title updated and reconciled)"
echo "CS-Cart REST API 2.0 live smoke: all checks passed"
