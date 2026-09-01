#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "${repo_root}"

# This is the repository qualification gate. Provider live/sandbox calls are
# deliberately not faked here: they require non-production credentials and
# produce environment-specific evidence at release time.
go test ./internal/core/marketplaceoperations ./internal/app/api ./internal/app/worker ./internal/platform/connectors -run 'Test(GoldenPath|LifecycleRunner|MarketplaceOperation|BuildMarketplaceOrderCreate|MarketplaceOperation)' -count=1
echo "Order fulfillment golden path qualification: PASS (synthetic, provider-neutral)"
echo "Live marketplace/carrier/payment qualification: REQUIRED at release topology"
