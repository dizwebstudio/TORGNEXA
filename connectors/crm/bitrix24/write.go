package bitrix24

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func numericID(value string) (int64, error) {
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || n < 1 {
		return 0, sdk.ErrInvalidCRMWrite
	}
	return n, nil
}
func numericIDs(values []string) ([]int64, error) {
	out := make([]int64, 0, len(values))
	for _, v := range values {
		n, e := numericID(v)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, nil
}

func fieldsForWrite(req sdk.CRMEntityWriteRequest) (map[string]any, error) {
	f := map[string]any{"originatorId": "TORGNEXA", "originId": req.ExternalID}
	switch req.Kind {
	case sdk.CRMContact:
		f["name"] = req.FirstName
		f["secondName"] = req.MiddleName
		f["lastName"] = req.LastName
		if req.CompanyRemoteID != "" {
			n, e := numericID(req.CompanyRemoteID)
			if e != nil {
				return nil, e
			}
			f["companyId"] = n
		}
	case sdk.CRMCompany:
		f["title"] = req.Title
	case sdk.CRMLead, sdk.CRMDeal:
		f["title"] = req.Title
		if req.StageRemoteID != "" {
			f["stageId"] = req.StageRemoteID
		}
		if req.CompanyRemoteID != "" {
			n, e := numericID(req.CompanyRemoteID)
			if e != nil {
				return nil, e
			}
			f["companyId"] = n
		}
		if len(req.ContactRemoteIDs) > 0 {
			ids, e := numericIDs(req.ContactRemoteIDs)
			if e != nil {
				return nil, e
			}
			f["contactIds"] = ids
		}
		if req.Opportunity != "" {
			f["opportunity"] = json.Number(req.Opportunity)
		}
		if req.Currency != "" {
			f["currencyId"] = req.Currency
		}
		if req.Kind == sdk.CRMDeal && req.PipelineRemoteID != "" {
			n, e := numericID(req.PipelineRemoteID)
			if e != nil {
				return nil, e
			}
			f["categoryId"] = n
		}
	default:
		return nil, sdk.ErrInvalidCRMWrite
	}
	return f, nil
}

func (c *Connector) getEntity(ctx context.Context, cfg Configuration, cred credentials, k sdk.CRMEntityKind, id int64) (sdk.CRMEntity, error) {
	typeID, e := entityTypeID(k)
	if e != nil {
		return sdk.CRMEntity{}, e
	}
	resp, e := c.call(ctx, cfg, cred, "crm.item.get", map[string]any{"entityTypeId": typeID, "id": id})
	if e != nil {
		return sdk.CRMEntity{}, e
	}
	var env struct {
		Result struct {
			Item remoteItem `json:"item"`
		} `json:"result"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || string(env.Result.Item.ID) == "" {
		return sdk.CRMEntity{}, ErrInvalidResponse
	}
	return projectItem(k, env.Result.Item)
}
func (c *Connector) findEntityByOrigin(ctx context.Context, cfg Configuration, cred credentials, k sdk.CRMEntityKind, externalID string) (sdk.CRMEntity, bool, error) {
	typeID, e := entityTypeID(k)
	if e != nil {
		return sdk.CRMEntity{}, false, e
	}
	resp, e := c.call(ctx, cfg, cred, "crm.item.list", map[string]any{"entityTypeId": typeID, "select": selectFields(k), "filter": map[string]any{"=originatorId": "TORGNEXA", "=originId": externalID}, "start": 0})
	if e != nil {
		return sdk.CRMEntity{}, false, e
	}
	var env struct {
		Result struct {
			Items []remoteItem `json:"items"`
		} `json:"result"`
		Total int `json:"total"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || env.Total < 0 || env.Total > 1 || len(env.Result.Items) > 1 {
		return sdk.CRMEntity{}, false, ErrInvalidResponse
	}
	if len(env.Result.Items) == 0 {
		return sdk.CRMEntity{}, false, nil
	}
	item, e := projectItem(k, env.Result.Items[0])
	return item, e == nil, e
}

func entityMatches(req sdk.CRMEntityWriteRequest, item sdk.CRMEntity) bool {
	if req.Kind != item.Kind || req.ExternalID != item.ExternalID {
		return false
	}
	switch req.Kind {
	case sdk.CRMContact:
		return req.FirstName == item.FirstName && req.MiddleName == item.MiddleName && req.LastName == item.LastName && req.CompanyRemoteID == item.CompanyRemoteID
	case sdk.CRMCompany:
		return req.Title == item.Title
	case sdk.CRMLead:
		return req.Title == item.Title && req.StageRemoteID == item.StageRemoteID && req.CompanyRemoteID == item.CompanyRemoteID && equalStrings(req.ContactRemoteIDs, item.ContactRemoteIDs) && req.Opportunity == item.Opportunity && req.Currency == item.Currency
	case sdk.CRMDeal:
		return req.Title == item.Title && req.StageRemoteID == item.StageRemoteID && req.PipelineRemoteID == item.PipelineRemoteID && req.CompanyRemoteID == item.CompanyRemoteID && equalStrings(req.ContactRemoteIDs, item.ContactRemoteIDs) && req.Opportunity == item.Opportunity && req.Currency == item.Currency
	default:
		return false
	}
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func (c *Connector) UpsertCRMEntity(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.CRMEntityWriteRequest) (sdk.CRMWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "crm.entities.write") != nil || req.Validate() != nil {
		return sdk.CRMWriteReceipt{}, sdk.ErrInvalidCRMWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CRMWriteReceipt{}, e
	}
	fields, e := fieldsForWrite(req)
	if e != nil {
		return sdk.CRMWriteReceipt{}, e
	}
	var receipt sdk.CRMWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		var existing sdk.CRMEntity
		found := false
		if req.RemoteID != "" {
			id, pe := numericID(req.RemoteID)
			if pe != nil {
				return pe
			}
			existing, pe = c.getEntity(ctx, cfg, cred, req.Kind, id)
			if pe != nil {
				return pe
			}
			found = true
		} else {
			var pe error
			existing, found, pe = c.findEntityByOrigin(ctx, cfg, cred, req.Kind, req.ExternalID)
			if pe != nil {
				return pe
			}
		}
		if found && entityMatches(req, existing) {
			receipt = sdk.CRMWriteReceipt{RemoteID: existing.RemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		typeID, _ := entityTypeID(req.Kind)
		method := "crm.item.add"
		body := map[string]any{"entityTypeId": typeID, "fields": fields}
		targetID := int64(0)
		if found {
			targetID, _ = numericID(existing.RemoteID)
			method = "crm.item.update"
			body["id"] = targetID
		}
		resp, callErr := c.call(ctx, cfg, cred, method, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			var after sdk.CRMEntity
			var ok bool
			var re error
			if targetID > 0 {
				after, re = c.getEntity(ctx, cfg, cred, req.Kind, targetID)
				ok = re == nil
			} else {
				after, ok, re = c.findEntityByOrigin(ctx, cfg, cred, req.Kind, req.ExternalID)
			}
			if re == nil && ok && entityMatches(req, after) {
				receipt = sdk.CRMWriteReceipt{RemoteID: after.RemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		if targetID == 0 {
			var env struct {
				Result struct {
					Item struct {
						ID flexID `json:"id"`
					} `json:"item"`
				} `json:"result"`
			}
			if json.Unmarshal(resp.Body, &env) != nil || string(env.Result.Item.ID) == "" {
				return ErrInvalidResponse
			}
			targetID, callErr = numericID(string(env.Result.Item.ID))
			if callErr != nil {
				return ErrInvalidResponse
			}
		}
		after, re := c.getEntity(ctx, cfg, cred, req.Kind, targetID)
		if re != nil || !entityMatches(req, after) {
			return ErrInvalidResponse
		}
		receipt = sdk.CRMWriteReceipt{RemoteID: after.RemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, e
}

func productRowsSignature(rows []sdk.CRMProductRowWrite) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ProductRemoteID+"\x00"+r.Name+"\x00"+r.Price+"\x00"+r.Quantity+"\x00"+r.TaxRate+"\x00"+strconv.FormatBool(r.TaxIncluded))
	}
	sort.Strings(out)
	return out
}
func remoteRowsSignature(rows []sdk.CRMProductRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ProductRemoteID+"\x00"+r.Name+"\x00"+r.Price+"\x00"+r.Quantity+"\x00"+r.TaxRate+"\x00"+strconv.FormatBool(r.TaxIncluded))
	}
	sort.Strings(out)
	return out
}
func sameSignature(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (c *Connector) readAllRows(ctx context.Context, cfg Configuration, cred credentials, k sdk.CRMEntityKind, ownerID int64) ([]sdk.CRMProductRow, error) {
	all := []sdk.CRMProductRow{}
	for start := 0; start <= 100000; start += 50 {
		raw, total, e := c.listProductRows(ctx, cfg, cred, k, ownerID, start)
		if e != nil {
			return nil, e
		}
		for _, v := range raw {
			item, e := projectProductRow(k, v)
			if e != nil {
				return nil, e
			}
			all = append(all, item)
		}
		if start+50 >= total {
			return all, nil
		}
	}
	return nil, ErrInvalidResponse
}
func rowsPayload(req sdk.CRMProductRowsWriteRequest) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(req.Rows))
	for _, r := range req.Rows {
		m := map[string]any{"productName": r.Name, "price": json.Number(r.Price), "quantity": json.Number(r.Quantity), "taxIncluded": "N"}
		if r.ProductRemoteID != "" {
			id, e := numericID(r.ProductRemoteID)
			if e != nil {
				return nil, e
			}
			m["productId"] = id
		}
		if r.TaxRate != "" {
			m["taxRate"] = json.Number(r.TaxRate)
		}
		if r.TaxIncluded {
			m["taxIncluded"] = "Y"
		}
		out = append(out, m)
	}
	return out, nil
}
func (c *Connector) ReplaceCRMProductRows(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.CRMProductRowsWriteRequest) (sdk.CRMWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "crm.productrows.write") != nil || req.Validate() != nil {
		return sdk.CRMWriteReceipt{}, sdk.ErrInvalidCRMWrite
	}
	ownerID, e := numericID(req.OwnerRemoteID)
	if e != nil {
		return sdk.CRMWriteReceipt{}, e
	}
	ot, e := ownerType(req.OwnerKind)
	if e != nil {
		return sdk.CRMWriteReceipt{}, sdk.ErrInvalidCRMWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CRMWriteReceipt{}, e
	}
	payload, e := rowsPayload(req)
	if e != nil {
		return sdk.CRMWriteReceipt{}, e
	}
	desired := productRowsSignature(req.Rows)
	var receipt sdk.CRMWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		before, e := c.readAllRows(ctx, cfg, cred, req.OwnerKind, ownerID)
		if e == nil && sameSignature(desired, remoteRowsSignature(before)) {
			receipt = sdk.CRMWriteReceipt{RemoteID: req.OwnerRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		_, callErr := c.call(ctx, cfg, cred, "crm.item.productrow.set", map[string]any{"ownerId": ownerID, "ownerType": ot, "productRows": payload})
		if callErr != nil && !isAmbiguousWrite(callErr) {
			return callErr
		}
		after, re := c.readAllRows(ctx, cfg, cred, req.OwnerKind, ownerID)
		if re == nil && sameSignature(desired, remoteRowsSignature(after)) {
			receipt = sdk.CRMWriteReceipt{RemoteID: req.OwnerRemoteID, Applied: true, Reconciled: callErr != nil}
			return receipt.Validate()
		}
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		return ErrInvalidResponse
	})
	return receipt, e
}
