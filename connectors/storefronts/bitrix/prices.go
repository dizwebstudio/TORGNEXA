package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrPriceNotFound = errors.New("bitrix: price not found")

func (connector *Connector) listPrices(ctx context.Context, configuration Configuration, credential credentials, productID int64, offset int) ([]bitrixPrice, error) {
	filter := map[string]any{"catalogGroupId": configuration.PriceTypeID}
	if productID > 0 {
		filter["productId"] = productID
	}
	response, err := connector.call(ctx, configuration, credential, "catalog.price.list", map[string]any{
		"select": []string{"id", "productId", "catalogGroupId", "price", "currency", "timestampX"},
		"filter": filter,
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, err
	}
	return decodePriceList(response.Body)
}

func (connector *Connector) pricePageItem(configuration Configuration, price bitrixPrice) (sdk.RemotePrice, error) {
	if price.ID < 1 || price.ProductID < 1 || price.CatalogGroupID != configuration.PriceTypeID || price.Currency != configuration.StoreCurrency {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	value, err := normalizeBitrixMoney(price.Price)
	if err != nil {
		return sdk.RemotePrice{}, err
	}
	updated, err := parseBitrixTime(price.TimestampX)
	if err != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	item := sdk.RemotePrice{VariantRemoteID: strconv.FormatInt(price.ProductID, 10), Value: value, Currency: price.Currency, UpdatedAt: updated}
	if item.Validate() != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	return item, nil
}

// ReadPrices reads the configured regular price type. The cursor is an opaque
// catalog.price.list offset bound to the non-secret site configuration.
func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(50) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil || configuration.PriceTypeID < 1 {
		return sdk.PricePage{}, ErrInvalidConfiguration
	}
	offset, err := decodePageCursor(request.Cursor, configuration.fingerprint("prices"))
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		prices, listErr := connector.listPrices(ctx, configuration, credential, 0, offset)
		if listErr != nil {
			return listErr
		}
		items := make([]sdk.RemotePrice, 0, len(prices))
		for _, price := range prices {
			item, itemErr := connector.pricePageItem(configuration, price)
			if itemErr != nil {
				return itemErr
			}
			items = append(items, item)
		}
		if len(prices) == 50 {
			cursor, cursorErr := encodePageCursor(offset+len(prices), configuration.fingerprint("prices"))
			if cursorErr != nil {
				return cursorErr
			}
			output.NextCursor = cursor
		}
		if len(items) > request.Limit {
			items = items[:request.Limit]
		}
		output.Items = items
		return output.Validate(request.Limit)
	})
	return output, err
}

func (connector *Connector) findPrice(ctx context.Context, configuration Configuration, credential credentials, productID int64) (bitrixPrice, error) {
	prices, err := connector.listPrices(ctx, configuration, credential, productID, 0)
	if err != nil {
		return bitrixPrice{}, err
	}
	var found bitrixPrice
	for _, price := range prices {
		if price.ProductID != productID || price.CatalogGroupID != configuration.PriceTypeID {
			continue
		}
		if found.ID != 0 {
			return bitrixPrice{}, ErrInvalidResponse
		}
		found = price
	}
	if found.ID == 0 {
		return bitrixPrice{}, ErrPriceNotFound
	}
	return found, nil
}

func priceMatches(configuration Configuration, price bitrixPrice, productID int64, value string) bool {
	current, err := normalizeBitrixMoney(price.Price)
	return err == nil && price.ID > 0 && price.ProductID == productID && price.CatalogGroupID == configuration.PriceTypeID && price.Currency == configuration.StoreCurrency && current == value
}

func (connector *Connector) reconcilePrice(ctx context.Context, configuration Configuration, credential credentials, productID int64) (bitrixPrice, bool, error) {
	price, err := connector.findPrice(ctx, configuration, credential, productID)
	if errors.Is(err, ErrPriceNotFound) {
		return bitrixPrice{}, false, nil
	}
	if err != nil {
		return bitrixPrice{}, false, err
	}
	return price, true, nil
}

// WritePrice updates or creates only the configured price type. It uses the
// narrow catalog.price.add/update endpoints so other price types are never
// deleted by an incomplete collection payload.
func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil || request.CompareAt != "" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	productID, err := strconv.ParseInt(request.VariantRemoteID, 10, 64)
	if err != nil || productID < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	if configuration.PriceTypeID < 1 || request.Currency != configuration.StoreCurrency {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	target, err := normalizeBitrixMoney(json.Number(request.Value))
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, exists, findErr := connector.reconcilePrice(ctx, configuration, credential, productID)
		if findErr != nil {
			return findErr
		}
		if exists && priceMatches(configuration, current, productID, target) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}

		var callErr error
		if exists {
			_, callErr = connector.call(ctx, configuration, credential, "catalog.price.update", map[string]any{
				"id":     current.ID,
				"fields": map[string]any{"price": json.Number(target), "currency": request.Currency},
			})
		} else {
			_, callErr = connector.call(ctx, configuration, credential, "catalog.price.add", map[string]any{
				"fields": map[string]any{"productId": productID, "catalogGroupId": configuration.PriceTypeID, "price": json.Number(target), "currency": request.Currency},
			})
		}
		if callErr != nil && !isAmbiguousWrite(callErr) {
			return callErr
		}
		verified, verifyExists, verifyErr := connector.reconcilePrice(ctx, configuration, credential, productID)
		if verifyErr == nil && verifyExists && priceMatches(configuration, verified, productID, target) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: callErr != nil}
			return receipt.Validate()
		}
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if verifyErr != nil {
			return verifyErr
		}
		return ErrInvalidResponse
	})
	return receipt, err
}
