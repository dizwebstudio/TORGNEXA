package moysklad

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type entityRef struct {
	Meta remoteMeta `json:"meta"`
}
type orderRow struct {
	Meta       remoteMeta `json:"meta"`
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Updated    string     `json:"updated"`
	Deleted    string     `json:"deleted,omitempty"`
	Applicable bool       `json:"applicable"`
	Store      *entityRef `json:"store,omitempty"`
	State      *entityRef `json:"state,omitempty"`
}

func (c *Connector) ReadERPOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ERPOrderPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxPageLimit) != nil {
		return sdk.ERPOrderPage{}, sdk.ErrInvalidReadRequest
	}
	fp := fingerprint("orders")
	cur, err := parseCursor(request.Cursor, fp)
	if err != nil || cur.Row != 0 || cur.Inner != 0 {
		return sdk.ERPOrderPage{}, sdk.ErrInvalidReadRequest
	}
	var response Response
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, err = c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: apiBasePath + "/entity/customerorder", Query: pageQuery(request.Limit, cur.Offset, QueryParam{"order", "updated"}), Token: token, AcceptGzip: true})
		if err != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ERPOrderPage{}, err
	}
	envelope, err := decodeListEnvelope(response.Body, request.Limit, cur.Offset)
	if err != nil {
		return sdk.ERPOrderPage{}, err
	}
	items := make([]sdk.ERPOrder, 0, len(envelope.Rows))
	seen := map[string]struct{}{}
	for _, raw := range envelope.Rows {
		var row orderRow
		if decodeObject(raw, &row) != nil || !safeText(row.ID, 128, true) || !safeText(row.Name, 300, true) || !safeText(row.Updated, 256, true) || !safeText(row.Deleted, 256, false) {
			return sdk.ERPOrderPage{}, ErrInvalidResponse
		}
		id, e := idFromMetaHref(row.Meta.Href, "customerorder")
		if e != nil || id != row.ID {
			return sdk.ERPOrderPage{}, ErrInvalidResponse
		}
		if _, ok := seen[id]; ok {
			return sdk.ERPOrderPage{}, ErrInvalidResponse
		}
		seen[id] = struct{}{}
		status, location := "", ""
		if row.State != nil {
			status, e = idFromMetaHref(row.State.Meta.Href, "customerorder/metadata/states")
			if e != nil {
				return sdk.ERPOrderPage{}, ErrInvalidResponse
			}
		}
		if row.Store != nil {
			location, e = idFromMetaHref(row.Store.Meta.Href, "store")
			if e != nil {
				return sdk.ERPOrderPage{}, ErrInvalidResponse
			}
		}
		item := sdk.ERPOrder{RemoteID: id, Number: row.Name, Revision: row.Updated, StatusRemoteID: status, LocationRemoteID: location, Applicable: row.Applicable, Deleted: row.Deleted != ""}
		if item.Validate() != nil {
			return sdk.ERPOrderPage{}, ErrInvalidResponse
		}
		items = append(items, item)
	}
	page := sdk.ERPOrderPage{Items: items}
	if cur.Offset+len(items) < envelope.Meta.Size {
		next, e := makeCursor(cursor{Offset: cur.Offset + len(items), Fingerprint: fp})
		if e != nil || len(items) == 0 {
			return sdk.ERPOrderPage{}, ErrInvalidResponse
		}
		page.NextCursor = next
	}
	if page.Validate(request.Limit) != nil {
		return sdk.ERPOrderPage{}, ErrInvalidResponse
	}
	return page, nil
}
