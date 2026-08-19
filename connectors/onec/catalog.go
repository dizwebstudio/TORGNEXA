package onec

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadERPCatalog(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ERPCatalogPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxPageLimit) != nil {
		return sdk.ERPCatalogPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ERPCatalogPage{}, err
	}
	fingerprint := configFingerprint(configuration, "catalog")
	offset, err := parseCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ERPCatalogPage{}, sdk.ErrInvalidReadRequest
	}
	mapping := configuration.Catalog
	query := pageQuery(request.Limit, offset,
		joinFields(mapping.IDField, mapping.CodeField, mapping.SKUField, mapping.TitleField, mapping.BrandField, mapping.RevisionField, mapping.ArchivedField),
		mapping.IDField+" asc")

	var response Response
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(username, password []byte) error {
		response, err = connector.transport.Do(ctx, Request{Method: "GET", Host: configuration.Host, Path: configuration.BasePath + "/" + mapping.Resource, Query: query, Username: username, Password: password})
		if err != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ERPCatalogPage{}, err
	}
	envelope, err := decodeEnvelope(response.Body, request.Limit)
	if err != nil {
		return sdk.ERPCatalogPage{}, err
	}
	items := make([]sdk.ERPProduct, 0, len(envelope.Value))
	for _, row := range envelope.Value {
		remoteID, parseErr := requiredString(row, mapping.IDField, 128)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		code, parseErr := requiredString(row, mapping.CodeField, 200)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		sku, parseErr := optionalString(row, mapping.SKUField, 200)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		title, parseErr := requiredString(row, mapping.TitleField, 500)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		brand, parseErr := optionalString(row, mapping.BrandField, 300)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		revision, parseErr := requiredString(row, mapping.RevisionField, 256)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		archived, parseErr := requiredBool(row, mapping.ArchivedField)
		if parseErr != nil {
			return sdk.ERPCatalogPage{}, parseErr
		}
		item := sdk.ERPProduct{RemoteID: remoteID, Code: code, SKU: sku, Title: title, Brand: brand, Revision: revision, Archived: archived}
		if item.Validate() != nil {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		items = append(items, item)
	}
	page := sdk.ERPCatalogPage{Items: items}
	if len(items) == request.Limit {
		next, cursorErr := makeCursor(offset+len(items), fingerprint)
		if cursorErr != nil || next == request.Cursor {
			return sdk.ERPCatalogPage{}, ErrInvalidResponse
		}
		page.NextCursor = next
	}
	if page.Validate(request.Limit) != nil {
		return sdk.ERPCatalogPage{}, ErrInvalidResponse
	}
	return page, nil
}
