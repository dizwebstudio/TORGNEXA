package yandexmarket

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type priceWriteResponse struct {
	Status string `json:"status"`
}

const maxYandexStockQuantity int64 = 2_000_000_000

type inventoryWriteResponse struct {
	Status string `json:"status"`
}

type partnerStockWriteBody struct {
	SKUItems []partnerStockWriteItem `json:"skuItems"`
}

type partnerStockWriteItem struct {
	SKU                string `json:"sku"`
	PartnerWarehouseID int64  `json:"partnerWarehouseId"`
	Count              int64  `json:"count"`
	UpdatedAt          string `json:"updatedAt"`
}

type campaignStockWriteBody struct {
	SKUs []campaignStockWriteSKU `json:"skus"`
}

type campaignStockWriteSKU struct {
	SKU   string                   `json:"sku"`
	Items []campaignStockWriteItem `json:"items"`
}

type campaignStockWriteItem struct {
	Count     int64  `json:"count"`
	UpdatedAt string `json:"updatedAt"`
}

type priceWriteBody struct {
	Offers []priceWriteOffer `json:"offers"`
}

type priceWriteOffer struct {
	OfferID string          `json:"offerId"`
	Price   priceWritePrice `json:"price"`
}

type priceWritePrice struct {
	Value        json.Number  `json:"value"`
	CurrencyID   string       `json:"currencyId"`
	DiscountBase *json.Number `json:"discountBase,omitempty"`
}

// WritePrice sets the exact desired seller price. Yandex Market documents these
// endpoints as asynchronous catalogue updates, so successful acceptance is not
// represented as read-after-write reconciliation; normal price reconciliation
// observes the eventual state later.
func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	currency, err := yandexCurrency(request.Currency)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	body := priceWriteBody{Offers: []priceWriteOffer{{OfferID: request.VariantRemoteID, Price: priceWritePrice{Value: json.Number(request.Value), CurrencyID: currency}}}}
	if request.CompareAt != "" {
		// The Yandex contract requires discountBase to be an integer.
		if strings.Contains(request.CompareAt, ".") {
			return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
		}
		compare := json.Number(request.CompareAt)
		body.Offers[0].Price.DiscountBase = &compare
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBodyBytes {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	path := campaignPath(configuration.CampaignID, "/offer-prices/updates")
	if configuration.PriceMode == PriceBusinessWide {
		path = businessPath(configuration.BusinessID, "/offer-prices/updates")
	}
	var requestID string
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Body: encoded, APIKey: key})
		if callErr != nil {
			// Setting an exact desired price is retry-safe, but the transport failure
			// leaves the remote outcome unknown. Surface a retryable error rather
			// than inventing success.
			return normalizedTransportError()
		}
		requestID = response.RequestID
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed priceWriteResponse
		if decodeUseNumber(response.Body, &parsed) != nil || parsed.Status != "OK" {
			return ErrInvalidResponse
		}
		return nil
	})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	receipt := sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: false}
	if receipt.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, errors.New("yandexmarket: invalid write receipt")
	}
	_ = requestID // request ids stay in transport/error telemetry and never affect canonical identity.
	return receipt, nil
}

// WriteInventory sends one exact available-stock value to the provider's
// documented warehouse endpoint. Yandex Market applies the update
// asynchronously, so acceptance is returned as Applied=true and is later
// confirmed by the normal inventory reconciliation scan.
func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil || request.Quantity > maxYandexStockQuantity || request.LocationRemoteID == "" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	warehouseID, err := strconv.ParseInt(request.LocationRemoteID, 10, 64)
	if err != nil || warehouseID < 1 || !configuredWarehouseAllowed(configuration, warehouseID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	updatedAt := connector.now().UTC()
	if updatedAt.IsZero() {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	updatedAtValue := updatedAt.Format(time.RFC3339Nano)
	var body any
	method := "POST"
	path := businessV3Path(configuration.BusinessID, "/offers/stocks/update")
	if configuration.InventoryMode == InventoryPartnerWarehouses {
		body = partnerStockWriteBody{SKUItems: []partnerStockWriteItem{{SKU: request.VariantRemoteID, PartnerWarehouseID: warehouseID, Count: request.Quantity, UpdatedAt: updatedAtValue}}}
	} else {
		method = "PUT"
		path = campaignPath(configuration.CampaignID, "/offers/stocks")
		body = campaignStockWriteBody{SKUs: []campaignStockWriteSKU{{SKU: request.VariantRemoteID, Items: []campaignStockWriteItem{{Count: request.Quantity, UpdatedAt: updatedAtValue}}}}}
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBodyBytes {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: method, Host: apiHost, Path: path, Body: encoded, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed inventoryWriteResponse
		if decodeUseNumber(response.Body, &parsed) != nil || parsed.Status != "OK" {
			return ErrInvalidResponse
		}
		return nil
	})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	receipt := sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: false}
	if receipt.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, errors.New("yandexmarket: invalid inventory write receipt")
	}
	return receipt, nil
}

func yandexCurrency(value string) (string, error) {
	switch value {
	case "RUB":
		return "RUR", nil
	case "RUR", "USD", "EUR", "UAH", "AUD", "GBP", "BYR", "BYN", "DKK", "ISK", "KZT", "CAD", "CNY", "NOK", "XDR", "SGD", "TRY", "SEK", "CHF", "JPY", "AZN", "ALL", "DZD", "AOA", "ARS", "AMD", "AFN", "BHD", "BGN", "BOB", "BWP", "BND", "BRL", "BIF", "HUF", "VEF", "KPW", "VND", "GMD", "GHS", "GNF", "HKD", "GEL", "AED", "EGP", "ZMK", "ILS", "INR", "IDR", "JOD", "IQD", "IRR", "YER", "QAR", "KES", "KGS", "COP", "CDF", "CRC", "KWD", "CUP", "LAK", "LVL", "SLL", "LBP", "LYD", "SZL", "LTL", "MUR", "MRO", "MKD", "MWK", "MGA", "MYR", "MAD", "MXN", "MZN", "MDL", "MNT", "NPR", "NGN", "NIO", "NZD", "OMR", "PKR", "PYG", "PEN", "PLN", "KHR", "SAR", "RON", "SCR", "SYP", "SKK", "SOS", "SDG", "SRD", "TJS", "THB", "TWD", "BDT", "TZS", "TND", "TMM", "UGX", "UZS", "UYU", "PHP", "DJF", "XAF", "XOF", "HRK", "CZK", "CLP", "LKR", "EEK", "ETB", "RSD", "ZAR", "KRW", "NAD", "TL", "UE":
		return value, nil
	default:
		return "", sdk.ErrInvalidCommerceWrite
	}
}
