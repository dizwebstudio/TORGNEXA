package bitrix24

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const bitrixPageSize = 50

func entityTypeID(k sdk.CRMEntityKind) (int, error) {
	switch k {
	case sdk.CRMLead:
		return 1, nil
	case sdk.CRMDeal:
		return 2, nil
	case sdk.CRMContact:
		return 3, nil
	case sdk.CRMCompany:
		return 4, nil
	default:
		return 0, sdk.ErrInvalidReadRequest
	}
}
func ownerType(k sdk.CRMEntityKind) (string, error) {
	switch k {
	case sdk.CRMLead:
		return "L", nil
	case sdk.CRMDeal:
		return "D", nil
	default:
		return "", sdk.ErrInvalidReadRequest
	}
}
func selectFields(k sdk.CRMEntityKind) []string {
	base := []string{"id", "originatorId", "originId", "createdTime", "updatedTime"}
	switch k {
	case sdk.CRMLead:
		return append(base, "title", "stageId", "companyId", "contactIds", "opportunity", "currencyId")
	case sdk.CRMDeal:
		return append(base, "title", "stageId", "categoryId", "companyId", "contactIds", "opportunity", "currencyId")
	case sdk.CRMContact:
		return append(base, "name", "secondName", "lastName", "companyId")
	case sdk.CRMCompany:
		return append(base, "title")
	default:
		return base
	}
}

func projectItem(k sdk.CRMEntityKind, v remoteItem) (sdk.CRMEntity, error) {
	created, e := parseRemoteTime(v.CreatedTime)
	if e != nil {
		return sdk.CRMEntity{}, e
	}
	updated, e := parseRemoteTime(v.UpdatedTime)
	if e != nil {
		return sdk.CRMEntity{}, e
	}
	contacts := make([]string, 0, len(v.ContactIDs)+1)
	seen := map[string]struct{}{}
	for _, id := range v.ContactIDs {
		s := string(id)
		if s != "" {
			if _, ok := seen[s]; !ok {
				contacts = append(contacts, s)
				seen[s] = struct{}{}
			}
		}
	}
	if s := string(v.ContactID); s != "" {
		if _, ok := seen[s]; !ok {
			contacts = append(contacts, s)
		}
	}
	external := ""
	if v.OriginatorID == "TORGNEXA" {
		external = v.OriginID
	}
	item := sdk.CRMEntity{RemoteID: string(v.ID), Kind: k, ExternalID: external, Title: v.Title, FirstName: v.Name, MiddleName: v.SecondName, LastName: v.LastName, StageRemoteID: v.StageID, PipelineRemoteID: string(v.CategoryID), CompanyRemoteID: string(v.CompanyID), ContactRemoteIDs: contacts, Opportunity: string(v.Opportunity), Currency: v.CurrencyID, CreatedAt: created, UpdatedAt: updated}
	if item.Validate() != nil {
		return sdk.CRMEntity{}, ErrInvalidResponse
	}
	return item, nil
}

func (c *Connector) listEntities(ctx context.Context, cfg Configuration, cred credentials, k sdk.CRMEntityKind, start int) ([]remoteItem, int, error) {
	type env struct {
		Result struct {
			Items []remoteItem `json:"items"`
		} `json:"result"`
		Total int `json:"total"`
	}
	typeID, e := entityTypeID(k)
	if e != nil {
		return nil, 0, e
	}
	resp, e := c.call(ctx, cfg, cred, "crm.item.list", map[string]any{"entityTypeId": typeID, "select": selectFields(k), "order": map[string]string{"id": "ASC"}, "start": start})
	if e != nil {
		return nil, 0, e
	}
	var out env
	if json.Unmarshal(resp.Body, &out) != nil || out.Total < 0 || len(out.Result.Items) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return out.Result.Items, out.Total, nil
}

func (c *Connector) ReadCRMEntities(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.CRMEntityQuery) (sdk.CRMEntityPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "crm.entities.read") != nil || q.Validate(50) != nil {
		return sdk.CRMEntityPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CRMEntityPage{}, e
	}
	fp := cfg.fingerprint("entities:" + string(q.Kind))
	cur, e := decodeCursor(q.Page.Cursor, fp)
	if e != nil {
		return sdk.CRMEntityPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.CRMEntityPage
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		raw, total, e := c.listEntities(ctx, cfg, cred, q.Kind, cur.Start)
		if e != nil {
			return e
		}
		if cur.Skip > len(raw) {
			return ErrInvalidResponse
		}
		end := cur.Skip + q.Page.Limit
		if end > len(raw) {
			end = len(raw)
		}
		items := make([]sdk.CRMEntity, 0, end-cur.Skip)
		for _, v := range raw[cur.Skip:end] {
			item, e := projectItem(q.Kind, v)
			if e != nil {
				return e
			}
			items = append(items, item)
		}
		next, e := nextCursor(cur.Start, cur.Skip, len(items), total, q.Page.Limit, fp)
		if e != nil {
			return e
		}
		out = sdk.CRMEntityPage{Items: items, NextCursor: next}
		return out.Validate(q.Page.Limit)
	})
	return out, e
}

func (c *Connector) listProductRows(ctx context.Context, cfg Configuration, cred credentials, k sdk.CRMEntityKind, ownerID int64, start int) ([]remoteProductRow, int, error) {
	ot, e := ownerType(k)
	if e != nil {
		return nil, 0, e
	}
	resp, e := c.call(ctx, cfg, cred, "crm.item.productrow.list", map[string]any{"filter": map[string]any{"=ownerType": ot, "=ownerId": ownerID}, "order": map[string]string{"id": "ASC"}, "start": start})
	if e != nil {
		return nil, 0, e
	}
	var env struct {
		Result struct {
			ProductRows []remoteProductRow `json:"productRows"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || env.Total < 0 || len(env.Result.ProductRows) > 50 {
		return nil, 0, ErrInvalidResponse
	}
	return env.Result.ProductRows, env.Total, nil
}
func projectProductRow(k sdk.CRMEntityKind, r remoteProductRow) (sdk.CRMProductRow, error) {
	item := sdk.CRMProductRow{RemoteID: string(r.ID), OwnerKind: k, OwnerRemoteID: string(r.OwnerID), ProductRemoteID: string(r.ProductID), Name: r.ProductName, Price: string(r.Price), Quantity: string(r.Quantity), TaxRate: string(r.TaxRate), TaxIncluded: r.TaxIncluded == "Y"}
	if item.Validate() != nil {
		return sdk.CRMProductRow{}, ErrInvalidResponse
	}
	return item, nil
}
func (c *Connector) ReadCRMProductRows(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.CRMProductRowQuery) (sdk.CRMProductRowPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "crm.productrows.read") != nil || q.Validate(50) != nil {
		return sdk.CRMProductRowPage{}, sdk.ErrInvalidReadRequest
	}
	ownerID, e := strconv.ParseInt(q.OwnerRemoteID, 10, 64)
	if e != nil || ownerID < 1 {
		return sdk.CRMProductRowPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CRMProductRowPage{}, e
	}
	fp := cfg.fingerprint("productrows:" + string(q.OwnerKind) + ":" + q.OwnerRemoteID)
	cur, e := decodeCursor(q.Page.Cursor, fp)
	if e != nil {
		return sdk.CRMProductRowPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.CRMProductRowPage
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		raw, total, e := c.listProductRows(ctx, cfg, cred, q.OwnerKind, ownerID, cur.Start)
		if e != nil {
			return e
		}
		if cur.Skip > len(raw) {
			return ErrInvalidResponse
		}
		end := cur.Skip + q.Page.Limit
		if end > len(raw) {
			end = len(raw)
		}
		items := make([]sdk.CRMProductRow, 0, end-cur.Skip)
		for _, v := range raw[cur.Skip:end] {
			item, e := projectProductRow(q.OwnerKind, v)
			if e != nil {
				return e
			}
			items = append(items, item)
		}
		next, e := nextCursor(cur.Start, cur.Skip, len(items), total, q.Page.Limit, fp)
		if e != nil {
			return e
		}
		out = sdk.CRMProductRowPage{Items: items, NextCursor: next}
		return out.Validate(q.Page.Limit)
	})
	return out, e
}
