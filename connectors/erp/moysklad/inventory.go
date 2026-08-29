package moysklad

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type stockByStore struct {
	Meta      remoteMeta  `json:"meta"`
	Name      string      `json:"name"`
	Stock     json.Number `json:"stock"`
	Reserve   json.Number `json:"reserve"`
	InTransit json.Number `json:"inTransit"`
}
type stockRow struct {
	Meta         remoteMeta     `json:"meta"`
	StockByStore []stockByStore `json:"stockByStore"`
}

func (c *Connector) ReadERPInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ERPInventoryPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxPageLimit) != nil {
		return sdk.ERPInventoryPage{}, sdk.ErrInvalidReadRequest
	}
	fp := fingerprint("inventory")
	cur, err := parseCursor(request.Cursor, fp)
	if err != nil {
		return sdk.ERPInventoryPage{}, sdk.ErrInvalidReadRequest
	}
	remoteLimit := request.Limit
	if remoteLimit > 100 {
		remoteLimit = 100
	}
	var response Response
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, err = c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: apiBasePath + "/report/stock/bystore", Query: pageQuery(remoteLimit, cur.Offset, QueryParam{"groupBy", "product"}, QueryParam{"stockMode", "all"}), Token: token, AcceptGzip: true})
		if err != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ERPInventoryPage{}, err
	}
	envelope, err := decodeListEnvelope(response.Body, remoteLimit, cur.Offset)
	if err != nil {
		return sdk.ERPInventoryPage{}, err
	}
	items := make([]sdk.ERPInventory, 0, request.Limit)
	seen := map[string]struct{}{}
	rowIndex, inner := cur.Row, cur.Inner
	if rowIndex > len(envelope.Rows) {
		return sdk.ERPInventoryPage{}, ErrInvalidResponse
	}
	for rowIndex < len(envelope.Rows) && len(items) < request.Limit {
		var row stockRow
		if decodeObject(envelope.Rows[rowIndex], &row) != nil || len(row.StockByStore) > 10000 {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		productID, e := idFromMetaHref(row.Meta.Href, "product")
		if e != nil {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		if inner > len(row.StockByStore) {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		for inner < len(row.StockByStore) && len(items) < request.Limit {
			stock := row.StockByStore[inner]
			locationID, e := idFromMetaHref(stock.Meta.Href, "store")
			if e != nil || !safeText(stock.Name, 300, true) {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			quantity, e := exactJSONDecimal(stock.Stock)
			if e != nil {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			if _, e = exactJSONDecimal(stock.Reserve); e != nil {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			if _, e = exactJSONDecimal(stock.InTransit); e != nil {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			item := sdk.ERPInventory{LocationRemoteID: locationID, ProductRemoteID: productID, Quantity: quantity}
			if item.Validate() != nil {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			key := locationID + "\x00" + productID
			if _, ok := seen[key]; ok {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			seen[key] = struct{}{}
			items = append(items, item)
			inner++
		}
		if inner >= len(row.StockByStore) {
			rowIndex++
			inner = 0
		}
	}
	page := sdk.ERPInventoryPage{Items: items}
	hasMoreCurrent := rowIndex < len(envelope.Rows)
	hasMoreRemote := cur.Offset+len(envelope.Rows) < envelope.Meta.Size
	if hasMoreCurrent || hasMoreRemote {
		nextCur := cursor{Offset: cur.Offset, Row: rowIndex, Inner: inner, Fingerprint: fp}
		if !hasMoreCurrent {
			if len(envelope.Rows) == 0 {
				return sdk.ERPInventoryPage{}, ErrInvalidResponse
			}
			nextCur.Offset = cur.Offset + len(envelope.Rows)
			nextCur.Row = 0
			nextCur.Inner = 0
		}
		next, e := makeCursor(nextCur)
		if e != nil {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		page.NextCursor = next
	}
	if page.Validate(request.Limit) != nil {
		return sdk.ERPInventoryPage{}, ErrInvalidResponse
	}
	return page, nil
}
