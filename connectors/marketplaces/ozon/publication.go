package ozon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type ozonPublicationBody struct {
	Items []ozonPublicationItem `json:"items"`
}

type ozonPublicationItem struct {
	OfferID       string `json:"offer_id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	CategoryID    int64  `json:"category_id"`
	Barcode       string `json:"barcode,omitempty"`
	Price         string `json:"price"`
	VAT           string `json:"vat,omitempty"`
	Weight        int64  `json:"weight,omitempty"`
	DimensionUnit string `json:"dimension_unit,omitempty"`
	Depth         int64  `json:"depth,omitempty"`
	Height        int64  `json:"height,omitempty"`
	Width         int64  `json:"width,omitempty"`
}

type ozonImportResponse struct {
	Result struct {
		TaskID json.RawMessage `json:"task_id"`
	} `json:"result"`
}

type ozonImportInfoResponse struct {
	Result struct {
		Status string `json:"status"`
	} `json:"result"`
}

// WriteProductPublication implements Ozon's asynchronous product import
// contract. Raw provider attributes and media URLs are intentionally not
// synthesized; those require a reviewed category/media bridge.
func (connector *Connector) WriteProductPublication(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductPublicationRequest) (sdk.ProductPublicationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	if request.DryRun {
		return sdk.ProductPublicationReceipt{Status: sdk.PublicationDryRun, ObservedAt: connector.now().UTC()}, nil
	}
	if request.Operation != marketplacepublication.OperationCreateProduct && request.Operation != marketplacepublication.OperationUpdateProduct && request.Operation != marketplacepublication.OperationUpdateVariant {
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
	item := ozonPublicationItem{OfferID: request.Snapshot.SKU, Name: request.Snapshot.Title, Description: request.Snapshot.Description, CategoryID: category, Price: decimalPrice(request.Snapshot.PriceMinor), VAT: request.Snapshot.VAT, Weight: request.Snapshot.Dimension.WeightG, DimensionUnit: "mm", Depth: request.Snapshot.Dimension.LengthMM, Height: request.Snapshot.Dimension.HeightMM, Width: request.Snapshot.Dimension.WidthMM}
	item.Barcode = request.Snapshot.GTIN
	if item.Barcode == "" && len(request.Snapshot.Variants) > 0 {
		item.Barcode = request.Snapshot.Variants[0].GTIN
		if item.Barcode == "" && len(request.Snapshot.Variants[0].Barcodes) > 0 {
			item.Barcode = request.Snapshot.Variants[0].Barcodes[0]
		}
	}
	body, err := json.Marshal(ozonPublicationBody{Items: []ozonPublicationItem{item}})
	if err != nil || len(body) > maxBodyBytes {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	var response Response
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		var callErr error
		response, callErr = connector.transport.Do(ctx, Request{Method: http.MethodPost, Host: apiHost, Path: "/v2/product/import", Body: body, ClientID: clientID, APIKey: apiKey, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	receipt := sdk.ProductPublicationReceipt{Status: sdk.PublicationAccepted, RemoteRequestID: response.RequestID, ObservedAt: connector.now().UTC()}
	var parsed ozonImportResponse
	if len(response.Body) > 0 && (json.Unmarshal(response.Body, &parsed) != nil || (len(parsed.Result.TaskID) > 0 && !validTaskID(parsed.Result.TaskID))) {
		return sdk.ProductPublicationReceipt{}, ErrInvalidResponse
	}
	if len(parsed.Result.TaskID) > 0 {
		receipt.RemoteOperationID = strings.Trim(string(parsed.Result.TaskID), `"`)
	}
	return receipt, receipt.Validate()
}

// ReadProductPublicationStatus resolves Ozon's import task without returning
// the provider response body to the application.
func (connector *Connector) ReadProductPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ProductPublicationStatusQuery) (sdk.ProductPublicationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || query.Validate() != nil || query.RemoteOperationID == "" {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	if _, err := strconv.ParseInt(query.RemoteOperationID, 10, 64); err != nil {
		return sdk.ProductPublicationReceipt{}, sdk.ErrInvalidProductPublication
	}
	body := []byte(`{"task_id":` + query.RemoteOperationID + `}`)
	var response Response
	err := connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		var callErr error
		response, callErr = connector.transport.Do(ctx, Request{Method: http.MethodPost, Host: apiHost, Path: "/v1/product/import/info", Body: body, ClientID: clientID, APIKey: apiKey, IdempotencyKey: query.IdempotencyKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		return normalizeHTTP(response)
	})
	if err != nil {
		return sdk.ProductPublicationReceipt{}, err
	}
	var parsed ozonImportInfoResponse
	if json.Unmarshal(response.Body, &parsed) != nil {
		return sdk.ProductPublicationReceipt{}, ErrInvalidResponse
	}
	status := sdk.PublicationUnknown
	switch strings.ToUpper(parsed.Result.Status) {
	case "SUCCESS", "COMPLETED", "DONE":
		status = sdk.PublicationPublished
	case "IN_PROGRESS", "PROCESSING", "PENDING":
		status = sdk.PublicationProcessing
	case "ERROR", "FAILED", "REJECTED":
		return sdk.ProductPublicationReceipt{Status: sdk.PublicationRejected, RemoteOperationID: query.RemoteOperationID, RemoteRequestID: response.RequestID, ErrorCode: "remote_rejected", ObservedAt: connector.now().UTC()}, nil
	}
	return sdk.ProductPublicationReceipt{Status: status, RemoteOperationID: query.RemoteOperationID, RemoteRequestID: response.RequestID, ObservedAt: connector.now().UTC()}, nil
}

func decimalPrice(minor int64) string {
	return strconv.FormatInt(minor/100, 10) + "." + strconv.FormatInt(minor%100+100, 10)[1:]
}

func validTaskID(value []byte) bool {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	if value[0] == '"' {
		return len(value) > 2 && value[len(value)-1] == '"' && validTaskID(value[1:len(value)-1])
	}
	for _, item := range value {
		if item < '0' || item > '9' {
			return false
		}
	}
	return true
}

func publicationUnsupported(code string) error {
	remote, err := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
	if err != nil {
		return errors.New("ozon: unsupported publication operation")
	}
	return remote
}

var _ sdk.ProductPublicationWriter = (*Connector)(nil)
var _ sdk.ProductPublicationStatusReader = (*Connector)(nil)
