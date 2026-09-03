package builtinruntime

import (
	ozon "github.com/torgnexa/torgnexa/connectors/marketplaces/ozon"
	wildberries "github.com/torgnexa/torgnexa/connectors/marketplaces/wildberries"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// MarketplaceSmokeProfile describes the bounded operations admitted by the
// external marketplace smoke runner. Provider-specific policy stays in this
// reviewed composition package; callers consume only the neutral capability.
type MarketplaceSmokeProfile struct {
	InventoryWrite bool
}

// MarketplaceSmokeProfileFor returns the reviewed smoke profile for a
// marketplace connector. Unknown connectors are never admitted by the smoke
// runner.
func MarketplaceSmokeProfileFor(connectorID string) (MarketplaceSmokeProfile, bool) {
	switch connectorID {
	case "wildberries":
		return MarketplaceSmokeProfile{}, true
	case "ozon":
		return MarketplaceSmokeProfile{InventoryWrite: true}, true
	default:
		return MarketplaceSmokeProfile{}, false
	}
}

// MarketplaceProductReader exposes the SDK product projection required by the
// bounded external smoke. Provider construction remains inside the reviewed
// built-in composition boundary, while remote IDs and variants stay in the
// provider-neutral SDK contract.
func (r *Registry) MarketplaceProductReader(account sdk.Account, runtime sdk.Runtime) (sdk.ProductReader, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "wildberries":
		return wildberries.New(wbHTTP{r.http}, nil), nil
	case "ozon":
		return ozon.New(ozonHTTP{r.http}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}
