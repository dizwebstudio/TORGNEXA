package moysklad

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type remoteMeta struct {
	Href         string `json:"href"`
	Type         string `json:"type,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	MetadataHref string `json:"metadataHref,omitempty"`
}
type assortmentRow struct {
	Meta     remoteMeta `json:"meta"`
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Code     string     `json:"code,omitempty"`
	Article  string     `json:"article,omitempty"`
	Updated  string     `json:"updated"`
	Archived bool       `json:"archived"`
}

func (c *Connector) ReadERPCatalog(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ERPCatalogPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxPageLimit) != nil {
		return sdk.ERPCatalogPage{}, sdk.ErrInvalidReadRequest
	}
	fp := fingerprint("catalog")
	cur, err := parseCursor(request.Cursor, fp)
	if err != nil || cur.Row != 0 || cur.Inner != 0 {
		return sdk.ERPCatalogPage{}, sdk.ErrInvalidReadRequest
	}
	var response Response
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, err = c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: apiBasePath + "/entity/assortment", Query: pageQuery(request.Limit, cur.Offset, QueryParam{"groupBy", "product"}, QueryParam{"order", "updated"}), Token: token, AcceptGzip: true})
		if err != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ERPCatalogPage{}, err
	}
	envelope, err := decodeListEnvelope(response.Body, request.Limit, cur.Offset)
	if err != nil {
		return sdk.ERPCatalogPage{}, err
	}
	items := make([]sdk.ERPProduct, 0, len(envelope.Rows))
	seen := map[string]struct{}{}
	for _, raw := range envelope.Rows {
		var row assortmentRow
		if decodeObject(raw, &row) != nil {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		if !safeText(row.ID, 128, true) || !safeText(row.Name, 500, true) || !safeText(row.Code, 200, false) || !safeText(row.Article, 200, false) || !safeText(row.Updated, 256, true) {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		id, parseErr := idFromMetaHref(row.Meta.Href, "product")
		if parseErr != nil || id != row.ID {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		if _, ok := seen[id]; ok {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		seen[id] = struct{}{}
		item := sdk.ERPProduct{RemoteID: id, Code: row.Code, SKU: row.Article, Title: row.Name, Revision: row.Updated, Archived: row.Archived}
		if item.Validate() != nil {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		items = append(items, item)
	}
	page := sdk.ERPCatalogPage{Items: items}
	if cur.Offset+len(items) < envelope.Meta.Size {
		next, e := makeCursor(cursor{Offset: cur.Offset + len(items), Fingerprint: fp})
		if e != nil || len(items) == 0 {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		page.NextCursor = next
	}
	if page.Validate(request.Limit) != nil {
		return sdk.ERPCatalogPage{}, ErrInvalidResponse
	}
	return page, nil
}
