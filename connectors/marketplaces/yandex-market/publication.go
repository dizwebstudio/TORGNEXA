package yandexmarket

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type ymPublicationBody struct {
	Offers []ymPublicationOffer `json:"offers"`
}

type ymPublicationOffer struct {
	OfferID          string   `json:"offerId"`
	Name             string   `json:"name"`
	Vendor           string   `json:"vendor,omitempty"`
	Description      string   `json:"description,omitempty"`
	MarketCategoryID int64    `json:"marketCategoryId"`
	Barcodes         []string `json:"barcodes,omitempty"`
}

// WriteProductPublication uses Yandex Market's business offer-mappings
// endpoint. The operation is accepted asynchronously by the remote catalogue.
// Media and localized category parameters stay behind explicit bridges and are
// rejected until their connector-specific qualification is installed.
func (connector *Connector) WriteProductPublication(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductPublicationRequest) (sdk.ProductPublicationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	if request.DryRun {
		return sdk.ProductPublicationReceipt{Status: sdk.PublicationDryRun, ObservedAt: connector.now().UTC()}, nil
	}
	if request.Operation != "create_product" && request.Operation != "update_product" && request.Operation != "update_variant" {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("operation_not_qualified")
	}
	if len(request.Snapshot.Media) > 0 {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("media_bridge_required")
	}
	if len(request.Snapshot.Attributes) > 0 {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("category_attribute_mapping_required")
	}
	category, err := strconv.ParseInt(request.Snapshot.CategoryCode, 10, 64)
	if err != nil || category < 1 {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("category_mapping_required")
	}
	barcodes := make([]string, 0, 1)
	if request.Snapshot.GTIN != "" {
		barcodes = append(barcodes, request.Snapshot.GTIN)
	} else if len(request.Snapshot.Variants) > 0 && len(request.Snapshot.Variants[0].Barcodes) > 0 {
		barcodes = append(barcodes, request.Snapshot.Variants[0].Barcodes[0])
	}
	body, err := json.Marshal(ymPublicationBody{Offers: []ymPublicationOffer{{OfferID: request.Snapshot.SKU, Name: request.Snapshot.Title, Vendor: request.Snapshot.Brand, Description: request.Snapshot.Description, MarketCategoryID: category, Barcodes: barcodes}}})
	if err != nil || len(body) > maxBodyBytes {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	var response Response
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		var callErr error
		response, callErr = connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: businessPath(configuration.BusinessID, "/offer-mappings/update"), Body: body, APIKey: key, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	receipt := sdk.ProductPublicationReceipt{Status: sdk.PublicationAccepted, RemoteID: request.Snapshot.SKU, RemoteRequestID: response.RequestID, ObservedAt: connector.now().UTC()}
	return receipt, receipt.Validate()
}

// ReadProductPublicationStatus resolves the normalized offer mapping. The
// remote API does not expose a provider-neutral operation id, so the offer id
// is used as the status lookup key.
func (connector *Connector) ReadProductPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ProductPublicationStatusQuery) (sdk.ProductPublicationReceipt, error) {
	if connector == nil || query.Validate() != nil || query.RemoteID == "" {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	page, err := connector.ReadProducts(ctx, account, runtime, sdk.PageRequest{Limit: 100})
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	for _, product := range page.Items {
		if product.RemoteID == query.RemoteID {
			return sdk.ProductPublicationReceipt{Status: sdk.PublicationPublished, RemoteID: query.RemoteID, ObservedAt: connector.now().UTC()}, nil
		}
	}
	return sdk.ProductPublicationReceipt{Status: sdk.PublicationUnknown, RemoteID: query.RemoteID, ObservedAt: connector.now().UTC()}, nil
}

func publicationUnsupported(code string) error {
	remote, err := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
	if err != nil {
		return errors.New("yandexmarket: unsupported publication operation")
	}
	return remote
}

var _ sdk.ProductPublicationWriter = (*Connector)(nil)
var _ sdk.ProductPublicationStatusReader = (*Connector)(nil)
