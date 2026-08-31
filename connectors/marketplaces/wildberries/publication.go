package wildberries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type wbPublicationCard struct {
	NmID            int64                         `json:"nmID,omitempty"`
	SubjectID       int64                         `json:"subjectID"`
	Variants        []wbPublicationVariant        `json:"variants"`
	VendorCode      string                        `json:"vendorCode"`
	Title           string                        `json:"title"`
	Description     string                        `json:"description,omitempty"`
	Brand           string                        `json:"brand,omitempty"`
	Characteristics []wbPublicationCharacteristic `json:"characteristics,omitempty"`
}

type wbPublicationVariant struct {
	VendorCode string              `json:"vendorCode"`
	Sizes      []wbPublicationSize `json:"sizes"`
}

type wbPublicationSize struct {
	TechSize string   `json:"techSize"`
	WbSize   string   `json:"wbSize"`
	Skus     []string `json:"skus"`
}

type wbPublicationCharacteristic struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WriteProductPublication implements the qualified WB product card surface.
// Card creation and full replacement are retry-safe when the host keeps the
// same idempotency key; unsupported remote lifecycle verbs fail closed.
func (connector *Connector) WriteProductPublication(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductPublicationRequest) (sdk.ProductPublicationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	if request.DryRun {
		return sdk.ProductPublicationReceipt{Status: sdk.PublicationDryRun, ObservedAt: connector.now().UTC()}, nil
	}
	if request.Operation != marketplacepublication.OperationCreateProduct && request.Operation != marketplacepublication.OperationUpdateProduct && request.Operation != marketplacepublication.OperationUpdateVariant && request.Operation != marketplacepublication.OperationUpdateAttributes {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("operation_not_qualified")
	}
	if len(request.Snapshot.Media) > 0 {
		return sdk.ProductPublicationReceipt{}, publicationUnsupported("media_bridge_required")
	}
	card, err := makeWBCard(request)
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	body, err := json.Marshal([]wbPublicationCard{card})
	if err != nil || len(body) > maxBodyBytes {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	path := "/content/v2/cards/upload"
	if request.Operation != marketplacepublication.OperationCreateProduct {
		path = "/content/v2/cards/update"
	}
	var response Response
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		var callErr error
		response, callErr = connector.transport.Do(ctx, Request{Method: http.MethodPost, Host: contentHost, Path: path, Body: body, Token: secret, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	receipt := sdk.ProductPublicationReceipt{Status: sdk.PublicationAccepted, RemoteRequestID: response.RequestID, ObservedAt: connector.now().UTC()}
	if card.NmID > 0 {
		receipt.RemoteID = strconv.FormatInt(card.NmID, 10)
	}
	return receipt, receipt.Validate()
}

// ReadProductPublicationStatus uses the existing normalized card reader. WB
// card creation is asynchronous, so absence on the first bounded page remains
// unknown instead of being presented as a rejection.
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

func makeWBCard(request sdk.ProductPublicationRequest) (wbPublicationCard, error) {
	category, err := strconv.ParseInt(request.Snapshot.CategoryCode, 10, 64)
	if err != nil || category < 1 {
		return wbPublicationCard{}, publicationUnsupported("category_mapping_required")
	}
	card := wbPublicationCard{SubjectID: category, VendorCode: request.Snapshot.SKU, Title: request.Snapshot.Title, Description: request.Snapshot.Description, Brand: request.Snapshot.Brand}
	if request.RemoteID != "" {
		card.NmID, err = strconv.ParseInt(request.RemoteID, 10, 64)
		if err != nil || card.NmID < 1 {
			return wbPublicationCard{}, sdk.ErrInvalidProductPublication
		}
	}
	variant := wbPublicationVariant{VendorCode: request.Snapshot.SKU}
	for _, item := range request.Snapshot.Variants {
		skus := append([]string(nil), item.Barcodes...)
		if len(skus) == 0 && item.GTIN != "" {
			skus = []string{item.GTIN}
		}
		if len(skus) == 0 {
			skus = []string{item.SKU}
		}
		variant.Sizes = append(variant.Sizes, wbPublicationSize{TechSize: "0", WbSize: "0", Skus: skus})
	}
	if len(variant.Sizes) == 0 {
		variant.Sizes = []wbPublicationSize{{TechSize: "0", WbSize: "0", Skus: []string{request.Snapshot.SKU}}}
	}
	card.Variants = []wbPublicationVariant{variant}
	for _, attribute := range request.Snapshot.Attributes {
		card.Characteristics = append(card.Characteristics, wbPublicationCharacteristic{Name: attribute.Code, Value: attribute.Value})
	}
	return card, nil
}

func publicationUnsupported(code string) error {
	remote, err := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
	if err != nil {
		return errors.New("wildberries: unsupported publication operation")
	}
	return fmt.Errorf("wildberries: %w", remote)
}

var _ sdk.ProductPublicationWriter = (*Connector)(nil)
var _ sdk.ProductPublicationStatusReader = (*Connector)(nil)
