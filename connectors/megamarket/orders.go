package megamarket

import (
	"context"
	"encoding/json"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"strconv"
)

type orderSearchResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		Total     int `json:"total"`
		Shipments []struct {
			ShipmentID string `json:"shipmentId"`
			OrderCode  string `json:"orderCode"`
			Status     string `json:"status"`
			StatusDate string `json:"statusDate"`
			CreatedAt  string `json:"createdAt"`
			Items      []struct {
				ItemIndex int    `json:"itemIndex"`
				GoodsID   string `json:"goodsId"`
				OfferID   string `json:"offerId"`
				Quantity  int64  `json:"quantity"`
			} `json:"items"`
		} `json:"shipments"`
	} `json:"data"`
}

func (c *Connector) ReadOrders(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PageRequest) (sdk.OrderPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || q.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.OrderPage{}, e
	}
	offset, e := parseOffsetCursor(q.Cursor, cfg.fingerprint("orders"))
	if e != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"merchantId": cfg.MerchantID, "limit": q.Limit, "offset": offset, "sort": map[string]any{"field": "statusDate", "order": "asc"}}})
	var out sdk.OrderPage
	e = c.withToken(ctx, r, a.SecretReference, func(k []byte) error {
		resp, ce := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/market/v1/orderService/order/search", Body: body, MerchantToken: k})
		if ce != nil {
			return normalizedTransportError()
		}
		if x := normalizeHTTP(resp); x != nil {
			return x
		}
		p, pe := parseOrders(resp.Body, q.Limit, offset, cfg)
		out = p
		return pe
	})
	return out, e
}
func parseOrders(body []byte, limit, offset int, cfg Configuration) (sdk.OrderPage, error) {
	var x orderSearchResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &x) != nil || !x.Success || x.Data == nil || len(x.Data.Shipments) > limit || x.Data.Total < offset+len(x.Data.Shipments) {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	items := make([]sdk.RemoteOrder, 0, len(x.Data.Shipments))
	seen := map[string]struct{}{}
	for _, s := range x.Data.Shipments {
		if !validText(s.ShipmentID, 200) || !validOptionalText(s.OrderCode, 300) || !validText(s.Status, 128) || len(s.Items) > 1000 {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		if _, ok := seen[s.ShipmentID]; ok {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		seen[s.ShipmentID] = struct{}{}
		created, e := parseUTC(s.CreatedAt)
		if e != nil {
			return sdk.OrderPage{}, e
		}
		updated, e := parseUTC(s.StatusDate)
		if e != nil || updated.Before(created) {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		oi := make([]sdk.RemoteOrderItem, 0, len(s.Items))
		for _, it := range s.Items {
			if it.ItemIndex < 0 || !validText(it.OfferID, 200) || !validText(it.GoodsID, 128) || it.Quantity < 1 {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			v := sdk.RemoteOrderItem{RemoteID: s.ShipmentID + ":" + strconv.Itoa(it.ItemIndex), VariantRemoteID: it.OfferID, Quantity: it.Quantity}
			if v.Validate() != nil {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			oi = append(oi, v)
		}
		o := sdk.RemoteOrder{RemoteID: s.ShipmentID, ExternalID: s.OrderCode, ProgramRemoteID: string(cfg.Scheme), StatusRemoteID: s.Status, CreatedAt: created, UpdatedAt: updated, Items: oi}
		if o.Validate() != nil {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		items = append(items, o)
	}
	next := ""
	if offset+len(items) < x.Data.Total && len(items) > 0 {
		var e error
		next, e = makeOffsetCursor(offset+len(items), cfg.fingerprint("orders"))
		if e != nil {
			return sdk.OrderPage{}, e
		}
	}
	p := sdk.OrderPage{Items: items, NextCursor: next}
	if p.Validate(limit) != nil {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	return p, nil
}
