package megamarket

import (
	"context"
	"encoding/json"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type stockResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		OfferID string `json:"offerId"`
		Stocks  []struct {
			WarehouseID string `json:"warehouseId"`
			Quantity    int64  `json:"quantity"`
		} `json:"stocks"`
	} `json:"data"`
}

func (c *Connector) ListInventoryLocations(ctx context.Context, a sdk.Account, r sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return nil, e
	}
	out := make([]sdk.RemoteLocation, 0, len(cfg.Warehouses))
	for _, w := range cfg.Warehouses {
		v := sdk.RemoteLocation{RemoteID: w.ID, Name: w.Name}
		if v.Validate() != nil {
			return nil, ErrInvalidConfiguration
		}
		out = append(out, v)
	}
	return out, nil
}
func (c *Connector) ReadInventory(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || q.Validate(100) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return nil, e
	}
	allowed := false
	for _, w := range cfg.Warehouses {
		if w.ID == q.LocationRemoteID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, sdk.ErrInvalidReadRequest
	}
	out := make([]sdk.RemoteInventory, 0, len(q.VariantRemoteIDs))
	e = c.withToken(ctx, r, a.SecretReference, func(k []byte) error {
		for _, offer := range q.VariantRemoteIDs {
			body, _ := json.Marshal(map[string]any{"offerId": offer})
			resp, ce := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/merchantIntegration/assortment/v1/stock/getByOfferId", Body: body, MerchantToken: k})
			if ce != nil {
				return normalizedTransportError()
			}
			if x := normalizeHTTP(resp); x != nil {
				return x
			}
			var parsed stockResponse
			if len(resp.Body) == 0 || json.Unmarshal(resp.Body, &parsed) != nil || !parsed.Success || parsed.Data == nil || parsed.Data.OfferID != offer {
				return ErrInvalidResponse
			}
			qty := int64(0)
			seen := map[string]struct{}{}
			for _, s := range parsed.Data.Stocks {
				if !validText(s.WarehouseID, 128) || s.Quantity < 0 {
					return ErrInvalidResponse
				}
				if _, ok := seen[s.WarehouseID]; ok {
					return ErrInvalidResponse
				}
				seen[s.WarehouseID] = struct{}{}
				if s.WarehouseID == q.LocationRemoteID {
					qty = s.Quantity
				}
			}
			v := sdk.RemoteInventory{LocationRemoteID: q.LocationRemoteID, VariantRemoteID: offer, Quantity: qty}
			if v.Validate() != nil {
				return ErrInvalidResponse
			}
			out = append(out, v)
		}
		return nil
	})
	return out, e
}
