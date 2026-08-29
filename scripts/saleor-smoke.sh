#!/usr/bin/env bash
set -euo pipefail

# Credentialed Saleor GraphQL smoke. This targets an operator-provided
# non-production store and deliberately keeps the token/response bodies out of
# stdout and logs. Saleor returns HTTP 200 for GraphQL auth/domain errors, so
# every response is checked for a top-level `errors` array as well as status.
graphql_url="${SALEOR_GRAPHQL_URL:-}"
token="${SALEOR_API_TOKEN:-}"
sku="${SALEOR_TEST_SKU:-}"
channel="${SALEOR_CHANNEL:-default-channel}"
warehouse="${SALEOR_WAREHOUSE:-warehouse}"
allow_http="${SALEOR_ALLOW_HTTP:-0}"
insecure_tls="${SALEOR_INSECURE_TLS:-0}"
allow_writes="${SALEOR_ALLOW_WRITES:-0}"
keep_changes="${SALEOR_KEEP_CHANGES:-0}"

if [[ -z "$graphql_url" || -z "$token" || -z "$sku" ]]; then
  cat >&2 <<'EOF'
Saleor live smoke requires SALEOR_GRAPHQL_URL, SALEOR_API_TOKEN and SALEOR_TEST_SKU.
Use a dedicated non-production Saleor App/admin bearer token and an existing
synthetic SKU; product creation is intentionally unsupported by the connector.
EOF
  exit 2
fi

python3 - "$graphql_url" "$allow_http" <<'PY'
from urllib.parse import urlsplit
import sys
u = urlsplit(sys.argv[1])
if u.scheme not in {"http", "https"} or not u.netloc:
    raise SystemExit("SALEOR_GRAPHQL_URL must be an absolute HTTP(S) URL")
if u.username or u.password or u.query or u.fragment:
    raise SystemExit("SALEOR_GRAPHQL_URL must not contain credentials, query or fragment")
if not u.path.rstrip("/").endswith("/graphql"):
    raise SystemExit("SALEOR_GRAPHQL_URL must point to Saleor's /graphql/ endpoint")
if u.scheme != "https" and sys.argv[2] != "1":
    raise SystemExit("HTTPS is required; set SALEOR_ALLOW_HTTP=1 only for an isolated local store")
if u.scheme == "http" and u.hostname not in {"127.0.0.1", "localhost", "::1"}:
    raise SystemExit("plain HTTP is restricted to loopback; use HTTPS for a remote store")
PY

if [[ ${#token} -lt 8 || ${#token} -gt 4096 || "$token" != "${token//[$'\n\r\t ']/}" ]]; then
  echo "SALEOR_API_TOKEN must be a non-whitespace token between 8 and 4096 characters" >&2
  exit 2
fi
if [[ ! "$sku" =~ ^[A-Za-z0-9._-]{1,200}$ ]]; then
  echo "SALEOR_TEST_SKU must contain only ASCII letters, digits, dot, underscore or hyphen" >&2
  exit 2
fi
if [[ ! "$channel" =~ ^[a-z0-9]+([a-z0-9-]*[a-z0-9])?$ ]]; then
  echo "SALEOR_CHANNEL must be a Saleor slug" >&2
  exit 2
fi
if [[ ! "$warehouse" =~ ^[a-z0-9]+([a-z0-9-]*[a-z0-9])?$ ]]; then
  echo "SALEOR_WAREHOUSE must be a Saleor slug" >&2
  exit 2
fi
for flag in allow_writes keep_changes; do
  value="${!flag}"
  if [[ "$value" != 0 && "$value" != 1 ]]; then
    echo "SALEOR_${flag^^} must be 0 or 1" >&2
    exit 2
  fi
done

tmp="$(mktemp -d)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
variant_id=""
product_id=""
channel_id=""
warehouse_id=""
original_sku=""
original_name=""
original_published=""
original_price=""
original_currency=""
original_quantity=""
changed_product=0
changed_price=0
changed_stock=0
restored=0

graphql() {
  local name="$1" query="$2" variables="$3" auth="${4:-yes}" status payload
  payload="$(python3 - "$query" "$variables" <<'PY'
import json, sys
query, variables = sys.argv[1:]
try:
    parsed = json.loads(variables)
except json.JSONDecodeError as exc:
    raise SystemExit(f"invalid GraphQL variables: {exc}")
print(json.dumps({"query": query, "variables": parsed}, separators=(",", ":")))
PY
)"
  local -a args=(--globoff --silent --show-error --connect-timeout 5 --max-time 30
    --request POST --output "$tmp/$name.body" --write-out '%{http_code}'
    --header 'Accept: application/json' --header 'Content-Type: application/json'
    --data "$payload")
  [[ "$insecure_tls" == 1 ]] && args+=(--insecure)
  [[ "$auth" == yes ]] && args+=(--header "Authorization: Bearer $token")
  status="$(curl "${args[@]}" "$graphql_url" || true)"
  printf '%s\n' "$status" > "$tmp/$name.status"
}

status_of() { cat "$tmp/$1.status"; }
assert_http_200() {
  local name="$1" got
  got="$(status_of "$name")"
  [[ "$got" == 200 ]] || { echo "FAIL $name: expected HTTP 200, got ${got:-unknown}" >&2; exit 1; }
}
assert_graphql_success() {
  local name="$1"
  assert_http_200 "$name"
  python3 - "$tmp/$name.body" <<'PY'
import json, sys
try:
    value = json.load(open(sys.argv[1], encoding="utf-8"))
except (OSError, ValueError) as exc:
    raise SystemExit(f"invalid Saleor GraphQL JSON: {exc}")
if value.get("errors"):
    first = value["errors"][0]
    code = first.get("extensions", {}).get("exception", {}).get("code", "unknown")
    raise SystemExit(f"Saleor GraphQL returned an error ({code})")
if value.get("data") is None:
    raise SystemExit("Saleor GraphQL response has no data")
PY
}
assert_graphql_error() {
  local name="$1"
  assert_http_200 "$name"
  python3 - "$tmp/$name.body" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if not value.get("errors"):
    raise SystemExit("unauthenticated Saleor request was unexpectedly accepted")
PY
}
assert_mutation_clean() {
  local name="$1" payload="$2"
  python3 - "$tmp/$name.body" "$payload" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
if value.get("errors"):
    raise SystemExit("Saleor mutation returned top-level GraphQL errors")
node = value.get("data", {}).get(sys.argv[2])
if not isinstance(node, dict):
    raise SystemExit("Saleor mutation payload is missing")
if node.get("errors"):
    raise SystemExit("Saleor mutation returned domain errors")
PY
}

restore_changes() {
  [[ "$allow_writes" == 1 && "$restored" == 0 ]] || return 0
  local ok=0 vars
  set +e
  if [[ "$changed_stock" == 1 && -n "$variant_id" && -n "$warehouse_id" ]]; then
    vars="$(python3 - "$variant_id" "$warehouse_id" "$original_quantity" <<'PY'
import json, sys
print(json.dumps({"variantId": sys.argv[1], "warehouse": sys.argv[2], "quantity": int(sys.argv[3])}, separators=(",", ":")))
PY
)"
    graphql restore_stock "$stock_query" "$vars" yes
    assert_mutation_clean restore_stock productVariantStocksUpdate || ok=1
  fi
  if [[ "$changed_price" == 1 && -n "$variant_id" && -n "$channel_id" ]]; then
    vars="$(python3 - "$variant_id" "$channel_id" "$original_price" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "channelId": sys.argv[2], "price": sys.argv[3]}, separators=(",", ":")))
PY
)"
    graphql restore_price "$price_query" "$vars" yes
    assert_mutation_clean restore_price productVariantChannelListingUpdate || ok=1
  fi
  if [[ "$changed_product" == 1 && -n "$variant_id" && -n "$product_id" && -n "$channel_id" ]]; then
    vars="$(python3 - "$variant_id" "$original_sku" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "sku": sys.argv[2]}, separators=(",", ":")))
PY
)"
    graphql restore_sku "$sku_query" "$vars" yes
    assert_mutation_clean restore_sku productVariantUpdate || ok=1
    vars="$(python3 - "$product_id" "$original_name" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "name": sys.argv[2]}, separators=(",", ":")))
PY
)"
    graphql restore_name "$name_query" "$vars" yes
    assert_mutation_clean restore_name productUpdate || ok=1
    vars="$(python3 - "$product_id" "$channel_id" "$original_published" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "channelId": sys.argv[2], "isPublished": sys.argv[3].lower() == "true"}, separators=(",", ":")))
PY
)"
    graphql restore_publication "$publication_query" "$vars" yes
    assert_mutation_clean restore_publication productChannelListingUpdate || ok=1
  fi
  set -e
  if [[ "$ok" == 0 ]]; then
    restored=1
  else
    echo "WARN Saleor cleanup failed; inspect the disposable store" >&2
  fi
}
cleanup() {
  local result=$?
  restore_changes
  rm -rf "$tmp"
  exit "$result"
}
trap cleanup EXIT

list_query='query ListVariants($channel: String!, $first: Int!) { productVariants(channel: $channel, first: $first) { edges { cursor node { id sku updatedAt product { id name } channelListings { channel { slug } price { amount currency } } } } pageInfo { hasNextPage endCursor } } }'
detail_query='query Variant($id: ID!, $channel: String!) { productVariant(id: $id, channel: $channel) { id sku product { id name channelListings { channel { slug } isPublished } } channelListings { channel { slug } price { amount currency } } stocks { warehouse { slug } quantity } } }'
channel_query='query Channel($slug: String!) { channel(slug: $slug) { id slug currencyCode } }'
warehouse_query='query Warehouse($slugs: [String!]) { warehouses(filter: { slugs: $slugs }, first: 1) { edges { node { id slug } } } }'
sku_query='mutation SetVariantSKU($id: ID!, $sku: String!) { productVariantUpdate(id: $id, input: { sku: $sku }) { productVariant { id sku } errors { field message code } } }'
name_query='mutation SetProductName($id: ID!, $name: String!) { productUpdate(id: $id, input: { name: $name }) { product { id name } errors { field message code } } }'
publication_query='mutation SetProductPublished($id: ID!, $channelId: ID!, $isPublished: Boolean!) { productChannelListingUpdate(id: $id, input: { updateChannels: [{ channelId: $channelId, isPublished: $isPublished }] }) { product { id } errors { field message code } } }'
price_query='mutation SetVariantPrice($id: ID!, $channelId: ID!, $price: PositiveDecimal!) { productVariantChannelListingUpdate(id: $id, input: [{ channelId: $channelId, price: $price }]) { variant { id } errors { field message code } } }'
stock_query='mutation SetStock($variantId: ID!, $warehouse: ID!, $quantity: Int!) { productVariantStocksUpdate(variantId: $variantId, stocks: [{ warehouse: $warehouse, quantity: $quantity }]) { productVariant { id } errors { field message code } } }'

echo "Saleor GraphQL smoke: $graphql_url"
list_vars="$(python3 - "$channel" <<'PY'
import json, sys
print(json.dumps({"channel": sys.argv[1], "first": 50}, separators=(",", ":")))
PY
)"
graphql unauthorized "$list_query" "$list_vars" no
assert_graphql_error unauthorized
echo "PASS unauthorized (GraphQL errors returned with HTTP 200)"

graphql products "$list_query" "$list_vars" yes
assert_graphql_success products
python3 - "$tmp/products.body" "$sku" "$tmp/selection.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
page = value.get("data", {}).get("productVariants", {})
edges = page.get("edges")
info = page.get("pageInfo")
if not isinstance(edges, list) or not isinstance(info, dict) or len(edges) > 50:
    raise SystemExit("Saleor productVariants response has invalid bounded shape")
for edge in edges:
    node = edge.get("node", {})
    if node.get("sku") == sys.argv[2]:
        product = node.get("product", {})
        if not node.get("id") or not product.get("id") or not product.get("name"):
            raise SystemExit("Saleor SKU mapping is incomplete")
        json.dump({"variant_id": node["id"], "product_id": product["id"]}, open(sys.argv[3], "w", encoding="utf-8"), separators=(",", ":"))
        raise SystemExit(0)
raise SystemExit("SALEOR_TEST_SKU was not found in the bounded productVariants page")
PY
variant_id="$(python3 - "$tmp/selection.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["variant_id"])
PY
)"
product_id="$(python3 - "$tmp/selection.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["product_id"])
PY
)"
echo "PASS catalog (SKU mapped to variant and parent product)"

detail_vars="$(python3 - "$variant_id" "$channel" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "channel": sys.argv[2]}, separators=(",", ":")))
PY
)"
graphql detail "$detail_query" "$detail_vars" yes
assert_graphql_success detail
python3 - "$tmp/detail.body" "$variant_id" "$product_id" "$sku" "$channel" "$warehouse" > "$tmp/detail.values" <<'PY'
import json, sys
from decimal import Decimal
value = json.load(open(sys.argv[1], encoding="utf-8"))["data"].get("productVariant")
if not value or value.get("id") != sys.argv[2] or value.get("sku") != sys.argv[4]:
    raise SystemExit("Saleor variant identity/SKU is invalid")
product = value.get("product", {})
if product.get("id") != sys.argv[3] or not product.get("name"):
    raise SystemExit("Saleor parent product is invalid")
listing = next((x for x in value.get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[5]), None)
if not listing or not listing.get("price") or listing["price"].get("amount") is None:
    raise SystemExit("Saleor variant has no price in configured channel")
price = str(listing["price"]["amount"])
try: format(Decimal(price), "f")
except Exception as exc: raise SystemExit(f"Saleor price is not decimal: {exc}")
currency = str(listing["price"].get("currency", ""))
if len(currency) != 3: raise SystemExit("Saleor price currency is invalid")
publication = next((x for x in product.get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[5]), None)
if not publication or not isinstance(publication.get("isPublished"), bool):
    raise SystemExit("Saleor product publication in configured channel is missing")
stock = next((x for x in value.get("stocks", []) if x.get("warehouse", {}).get("slug") == sys.argv[6]), None)
if not stock or not isinstance(stock.get("quantity"), int) or stock["quantity"] < 0:
    raise SystemExit("Saleor stock in configured warehouse is missing")
for item in [value["sku"], product["name"], ("true" if publication["isPublished"] else "false"), price, currency, str(stock["quantity"])]:
    print(item)
PY
{
  read -r original_sku
  read -r original_name
  read -r original_published
  read -r original_price
  read -r original_currency
  read -r original_quantity
} < "$tmp/detail.values"
echo "PASS detail (channel=$channel warehouse=$warehouse currency=$original_currency)"

channel_vars="$(python3 - "$channel" <<'PY'
import json, sys
print(json.dumps({"slug": sys.argv[1]}, separators=(",", ":")))
PY
)"
graphql channel "$channel_query" "$channel_vars" yes
assert_graphql_success channel
channel_id="$(python3 - "$tmp/channel.body" "$channel" "$original_currency" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))["data"].get("channel")
if not v or not v.get("id") or v.get("slug") != sys.argv[2] or str(v.get("currencyCode", "")).upper() != sys.argv[3].upper():
    raise SystemExit("Saleor channel resolution/currency is invalid")
print(v["id"])
PY
)"
echo "PASS channel (resolved channel id)"

warehouse_vars="$(python3 - "$warehouse" <<'PY'
import json, sys
print(json.dumps({"slugs": [sys.argv[1]]}, separators=(",", ":")))
PY
)"
graphql warehouse "$warehouse_query" "$warehouse_vars" yes
assert_graphql_success warehouse
warehouse_id="$(python3 - "$tmp/warehouse.body" "$warehouse" <<'PY'
import json, sys
edges = json.load(open(sys.argv[1], encoding="utf-8"))["data"].get("warehouses", {}).get("edges", [])
if len(edges) != 1 or edges[0].get("node", {}).get("slug") != sys.argv[2] or not edges[0]["node"].get("id"):
    raise SystemExit("Saleor warehouse resolution is invalid")
print(edges[0]["node"]["id"])
PY
)"
echo "PASS warehouse (resolved warehouse id)"

if [[ "$allow_writes" == 1 ]]; then
  new_sku="TORGNEXA-SALEOR-$run_id"
  new_name="TORGNEXA Saleor smoke $run_id"
  new_published="false"
  [[ "$original_published" == false ]] && new_published=true
  changed_product=1

  vars="$(python3 - "$variant_id" "$new_sku" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "sku": sys.argv[2]}, separators=(",", ":")))
PY
)"
  graphql product_sku_update "$sku_query" "$vars" yes
  assert_graphql_success product_sku_update
  assert_mutation_clean product_sku_update productVariantUpdate
  echo "PASS product_sku_update (HTTP 200/domain errors empty)"

  vars="$(python3 - "$product_id" "$new_name" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "name": sys.argv[2]}, separators=(",", ":")))
PY
)"
  graphql product_name_update "$name_query" "$vars" yes
  assert_graphql_success product_name_update
  assert_mutation_clean product_name_update productUpdate
  echo "PASS product_name_update (HTTP 200/domain errors empty)"

  vars="$(python3 - "$product_id" "$channel_id" "$new_published" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "channelId": sys.argv[2], "isPublished": sys.argv[3] == "true"}, separators=(",", ":")))
PY
)"
  graphql product_publication_update "$publication_query" "$vars" yes
  assert_graphql_success product_publication_update
  assert_mutation_clean product_publication_update productChannelListingUpdate
  echo "PASS product_publication_update (HTTP 200/domain errors empty)"

  new_price="$(python3 - "$original_price" <<'PY'
from decimal import Decimal
import sys
print(format(Decimal(sys.argv[1]) + Decimal("0.01"), "f"))
PY
)"
  vars="$(python3 - "$variant_id" "$channel_id" "$new_price" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "channelId": sys.argv[2], "price": sys.argv[3]}, separators=(",", ":")))
PY
)"
  changed_price=1
  graphql price_update "$price_query" "$vars" yes
  assert_graphql_success price_update
  assert_mutation_clean price_update productVariantChannelListingUpdate
  echo "PASS price_update (HTTP 200/domain errors empty)"

  new_quantity=$((original_quantity + 1))
  vars="$(python3 - "$variant_id" "$warehouse_id" "$new_quantity" <<'PY'
import json, sys
print(json.dumps({"variantId": sys.argv[1], "warehouse": sys.argv[2], "quantity": int(sys.argv[3])}, separators=(",", ":")))
PY
)"
  changed_stock=1
  graphql stock_update "$stock_query" "$vars" yes
  assert_graphql_success stock_update
  assert_mutation_clean stock_update productVariantStocksUpdate
  echo "PASS stock_update (HTTP 200/domain errors empty)"

  graphql after_write "$detail_query" "$detail_vars" yes
  assert_graphql_success after_write
  python3 - "$tmp/after_write.body" "$variant_id" "$product_id" "$channel" "$warehouse" "$new_sku" "$new_name" "$new_published" "$new_price" "$new_quantity" <<'PY'
import json, sys
from decimal import Decimal
v = json.load(open(sys.argv[1], encoding="utf-8"))["data"]["productVariant"]
if v.get("id") != sys.argv[2] or v.get("sku") != sys.argv[6] or v.get("product", {}).get("id") != sys.argv[3] or v["product"].get("name") != sys.argv[7]:
    raise SystemExit("Saleor product write was not reconciled")
listing = next((x for x in v.get("product", {}).get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[4]), None)
if not listing or listing.get("isPublished") != (sys.argv[8] == "true"):
    raise SystemExit("Saleor publication write was not reconciled")
price = next((x.get("price", {}).get("amount") for x in v.get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[4]), None)
if price is None or Decimal(str(price)) != Decimal(sys.argv[9]):
    raise SystemExit("Saleor price write was not reconciled")
stock = next((x.get("quantity") for x in v.get("stocks", []) if x.get("warehouse", {}).get("slug") == sys.argv[5]), None)
if stock != int(sys.argv[10]):
    raise SystemExit("Saleor stock write was not reconciled")
PY
  echo "PASS writes (product/price/stock read-after-write reconciled)"
  if [[ "$keep_changes" == 0 ]]; then
    restore_changes
    graphql after_restore "$detail_query" "$detail_vars" yes
    assert_graphql_success after_restore
    python3 - "$tmp/after_restore.body" "$variant_id" "$product_id" "$channel" "$warehouse" "$original_sku" "$original_name" "$original_published" "$original_price" "$original_quantity" <<'PY'
import json, sys
from decimal import Decimal
v = json.load(open(sys.argv[1], encoding="utf-8"))["data"]["productVariant"]
if v.get("id") != sys.argv[2] or v.get("sku") != sys.argv[6] or v.get("product", {}).get("id") != sys.argv[3] or v["product"].get("name") != sys.argv[7]:
    raise SystemExit("Saleor cleanup did not restore product")
listing = next((x for x in v.get("product", {}).get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[4]), None)
if not listing or listing.get("isPublished") != (sys.argv[8].lower() == "true"):
    raise SystemExit("Saleor cleanup did not restore publication")
price = next((x.get("price", {}).get("amount") for x in v.get("channelListings", []) if x.get("channel", {}).get("slug") == sys.argv[4]), None)
if price is None or Decimal(str(price)) != Decimal(sys.argv[9]):
    raise SystemExit("Saleor cleanup did not restore price")
stock = next((x.get("quantity") for x in v.get("stocks", []) if x.get("warehouse", {}).get("slug") == sys.argv[5]), None)
if stock != int(sys.argv[10]):
    raise SystemExit("Saleor cleanup did not restore stock")
PY
    echo "PASS cleanup (original product/price/stock restored)"
  else
    echo "INFO changes kept (SALEOR_KEEP_CHANGES=1)"
  fi
else
  echo "INFO writes skipped (set SALEOR_ALLOW_WRITES=1 on a disposable non-production store)"
fi

echo "Saleor GraphQL smoke: all checks passed"
