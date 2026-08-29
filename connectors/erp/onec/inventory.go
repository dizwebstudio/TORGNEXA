package onec

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadERPInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ERPInventoryPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxPageLimit) != nil {
		return sdk.ERPInventoryPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ERPInventoryPage{}, err
	}
	fingerprint := configFingerprint(configuration, "inventory")
	offset, err := parseCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ERPInventoryPage{}, sdk.ErrInvalidReadRequest
	}
	mapping := configuration.Inventory
	query := pageQuery(request.Limit, offset,
		joinFields(mapping.ProductField, mapping.LocationField, mapping.QuantityField),
		mapping.LocationField+" asc,"+mapping.ProductField+" asc")

	var response Response
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(username, password []byte) error {
		response, err = connector.transport.Do(ctx, Request{Method: "GET", Host: configuration.Host, Path: configuration.BasePath + "/" + mapping.Resource + "/" + mapping.Function + "()", Query: query, Username: username, Password: password})
		if err != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ERPInventoryPage{}, err
	}
	envelope, err := decodeEnvelope(response.Body, request.Limit)
	if err != nil {
		return sdk.ERPInventoryPage{}, err
	}
	items := make([]sdk.ERPInventory, 0, len(envelope.Value))
	for _, row := range envelope.Value {
		productID, parseErr := requiredString(row, mapping.ProductField, 128)
		if parseErr != nil {
			return sdk.ERPInventoryPage{}, parseErr
		}
		locationID, parseErr := requiredString(row, mapping.LocationField, 128)
		if parseErr != nil {
			return sdk.ERPInventoryPage{}, parseErr
		}
		quantity, parseErr := exactDecimal(row, mapping.QuantityField)
		if parseErr != nil {
			return sdk.ERPInventoryPage{}, parseErr
		}
		item := sdk.ERPInventory{LocationRemoteID: locationID, ProductRemoteID: productID, Quantity: quantity}
		if item.Validate() != nil {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		items = append(items, item)
	}
	page := sdk.ERPInventoryPage{Items: items}
	if len(items) == request.Limit {
		next, cursorErr := makeCursor(offset+len(items), fingerprint)
		if cursorErr != nil || next == request.Cursor {
			return sdk.ERPInventoryPage{}, ErrInvalidResponse
		}
		page.NextCursor = next
	}
	if page.Validate(request.Limit) != nil {
		return sdk.ERPInventoryPage{}, ErrInvalidResponse
	}
	return page, nil
}
