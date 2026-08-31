#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off

has_match() {
  if command -v rg >/dev/null 2>&1; then
    rg -q "$1" "$2"
  else
    grep -Eq "$1" "$2"
  fi
}

required=(
  adr/0169-marketplace-product-publication.md
  tasks/issues/217-marketplace-product-publication.md
  docs/operations/217-marketplace-product-publication.md
  migrations/000044_marketplace_product_publication.sql
  internal/core/marketplacepublication/publication.go
  internal/platform/connectors/product_publication.go
  internal/platform/postgres/marketplacepublicationrepo/repository.go
  internal/app/worker/marketplace_publication.go
  internal/app/api/marketplace_publication.go
)
for file in "${required[@]}"; do
  [[ -f "$file" ]] || { echo "missing marketplace publication qualification artifact: $file" >&2; exit 1; }
done

for provider in wildberries ozon yandex-market; do
  manifest="connectors/marketplaces/$provider/manifest.json"
  has_match 'products\.write' "$manifest" || { echo "products.write missing from $manifest" >&2; exit 1; }
done

if (command -v rg >/dev/null 2>&1 && rg -n 'https?://|Authorization|api[_-]?key|access[_-]?token' internal/core/marketplacepublication/publication.go internal/platform/connectors/product_publication.go) || (! command -v rg >/dev/null 2>&1 && grep -En 'https?://|Authorization|api[_-]?key|access[_-]?token' internal/core/marketplacepublication/publication.go internal/platform/connectors/product_publication.go); then
  echo "provider URL or credential material crossed the neutral publication boundary" >&2
  exit 1
fi

go test ./internal/core/marketplacepublication ./internal/platform/connectors ./internal/platform/postgres/marketplacepublicationrepo ./internal/platform/builtinruntime ./internal/app/api ./internal/app/worker ./connectors/marketplaces/wildberries ./connectors/marketplaces/ozon ./connectors/marketplaces/yandex-market
echo "Marketplace product publication qualification passed"
