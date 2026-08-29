package bitrix

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func inventoryDocumentNumber(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "TNX-" + hex.EncodeToString(digest[:])[:32]
}

func (connector *Connector) findInventoryDocument(ctx context.Context, configuration Configuration, credential credentials, documentNumber string) (bitrixDocument, bool, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.document.list", map[string]any{
		"select": []string{"id", "docType", "docNumber", "status"},
		"filter": map[string]any{"docNumber": documentNumber},
		"order":  map[string]string{"id": "asc"},
		"start":  0,
	})
	if err != nil {
		return bitrixDocument{}, false, err
	}
	documents, total, err := decodeDocumentList(response.Body)
	if err != nil || total > 50 || len(documents) > 1 {
		return bitrixDocument{}, false, ErrInvalidResponse
	}
	if len(documents) == 0 {
		return bitrixDocument{}, false, nil
	}
	document := documents[0]
	if document.ID < 1 || document.DocNumber != documentNumber || (document.DocType != "S" && document.DocType != "D") || (document.Status != "N" && document.Status != "Y" && document.Status != "C") {
		return bitrixDocument{}, false, ErrInvalidResponse
	}
	return document, true, nil
}

func (connector *Connector) listInventoryDocumentElements(ctx context.Context, configuration Configuration, credential credentials, documentID int64) ([]bitrixDocumentElement, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.document.element.list", map[string]any{
		"select": []string{"id", "docId", "elementId", "amount", "storeFrom", "storeTo"},
		"filter": map[string]any{"docId": documentID},
		"order":  map[string]string{"id": "asc"},
		"start":  0,
	})
	if err != nil {
		return nil, err
	}
	elements, total, err := decodeDocumentElementList(response.Body)
	if err != nil || total > 50 || len(elements) > 50 {
		return nil, ErrInvalidResponse
	}
	return elements, nil
}

func inventoryElementMatches(element bitrixDocumentElement, documentID, productID, storeID, delta int64, documentType string) bool {
	if element.ID < 1 || element.DocID != documentID || element.ElementID != productID || element.Amount.String() == "" {
		return false
	}
	amount, err := element.Amount.Int64()
	if err != nil || amount != delta || amount < 1 {
		return false
	}
	if documentType == "S" {
		return element.StoreFrom == nil && element.StoreTo != nil && *element.StoreTo == storeID
	}
	return documentType == "D" && element.StoreFrom != nil && *element.StoreFrom == storeID && element.StoreTo == nil
}

func (connector *Connector) ensureInventoryDocumentElement(ctx context.Context, configuration Configuration, credential credentials, document bitrixDocument, productID, storeID, delta int64) error {
	elements, err := connector.listInventoryDocumentElements(ctx, configuration, credential, document.ID)
	if err != nil {
		return err
	}
	if len(elements) > 1 {
		return ErrInvalidResponse
	}
	if len(elements) == 1 {
		if inventoryElementMatches(elements[0], document.ID, productID, storeID, delta, document.DocType) {
			return nil
		}
		return ErrInvalidResponse
	}
	fields := map[string]any{
		"docId":     document.ID,
		"elementId": productID,
		"amount":    json.Number(strconv.FormatInt(delta, 10)),
	}
	if document.DocType == "S" {
		fields["storeTo"] = storeID
	} else {
		fields["storeFrom"] = storeID
	}
	if _, err := connector.call(ctx, configuration, credential, "catalog.document.element.add", map[string]any{"fields": fields}); err != nil {
		return err
	}
	elements, err = connector.listInventoryDocumentElements(ctx, configuration, credential, document.ID)
	if err != nil || len(elements) != 1 || !inventoryElementMatches(elements[0], document.ID, productID, storeID, delta, document.DocType) {
		return ErrInvalidResponse
	}
	return nil
}

func inventoryWriteReconciled(ctx context.Context, connector *Connector, configuration Configuration, credential credentials, storeID, productID, target int64, reconciled bool) (sdk.CommerceWriteReceipt, error) {
	current, err := connector.inventoryQuantity(ctx, configuration, credential, storeID, productID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	if current != target {
		return sdk.CommerceWriteReceipt{}, writeOutcomeUnknown()
	}
	return sdk.CommerceWriteReceipt{RemoteID: strconv.FormatInt(productID, 10), Applied: !reconciled, Duplicate: reconciled, Reconciled: reconciled}, nil
}

func (connector *Connector) inventoryQuantity(ctx context.Context, configuration Configuration, credential credentials, storeID, productID int64) (int64, error) {
	rows, total, err := connector.listStoreProducts(ctx, configuration, credential, storeID, []int64{productID}, 0)
	if err != nil || total > 50 || len(rows) > 1 {
		return 0, ErrInvalidResponse
	}
	if len(rows) == 0 {
		return 0, nil
	}
	row := rows[0]
	if row.ID < 1 || row.ProductID != productID || row.StoreID != storeID {
		return 0, ErrInvalidResponse
	}
	quantity, err := row.Amount.Int64()
	if err != nil || quantity < 0 {
		return 0, ErrInvalidResponse
	}
	return quantity, nil
}

// WriteInventory applies an absolute integer quantity through Bitrix
// warehouse accounting: a positive delta is an "S" stock-receipt document,
// while a negative delta is a "D" write-off document. The document number is
// derived from the SDK idempotency key so a retry resumes a draft instead of
// creating a second adjustment.
func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil || request.LocationRemoteID == "" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	storeID, err := strconv.ParseInt(request.LocationRemoteID, 10, 64)
	if err != nil || storeID < 1 {
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
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		userID, parseErr := strconv.ParseInt(credential.UserID, 10, 64)
		if parseErr != nil || userID < 1 {
			return ErrInvalidCredentials
		}
		current, quantityErr := connector.inventoryQuantity(ctx, configuration, credential, storeID, productID)
		if quantityErr != nil {
			return quantityErr
		}
		if current == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		delta := request.Quantity - current
		if delta == 0 {
			return ErrInvalidResponse
		}
		documentType := "S"
		if delta < 0 {
			documentType = "D"
			delta = -delta
		}
		documentNumber := inventoryDocumentNumber(request.IdempotencyKey)
		document, exists, findErr := connector.findInventoryDocument(ctx, configuration, credential, documentNumber)
		if findErr != nil {
			return findErr
		}
		if exists {
			if document.DocType != documentType {
				return ErrInvalidResponse
			}
			if document.Status == "Y" || document.Status == "C" {
				if document.Status == "Y" {
					receipt, err = inventoryWriteReconciled(ctx, connector, configuration, credential, storeID, productID, request.Quantity, true)
					return err
				}
				return writeOutcomeUnknown()
			}
		} else {
			response, addErr := connector.call(ctx, configuration, credential, "catalog.document.add", map[string]any{"fields": map[string]any{
				"docType":       documentType,
				"currency":      configuration.StoreCurrency,
				"responsibleId": userID,
				"docNumber":     documentNumber,
				"title":         "TORGNEXA stock adjustment",
				"commentary":    "TORGNEXA idempotent inventory synchronization",
			}})
			if addErr != nil {
				if !isAmbiguousWrite(addErr) {
					return addErr
				}
				receipt, err = inventoryWriteReconciled(ctx, connector, configuration, credential, storeID, productID, request.Quantity, true)
				return err
			}
			var envelope struct {
				Result struct {
					Document bitrixDocument `json:"document"`
				} `json:"result"`
			}
			if json.Unmarshal(response.Body, &envelope) != nil || envelope.Result.Document.ID < 1 || envelope.Result.Document.DocType != documentType || envelope.Result.Document.Status != "N" || envelope.Result.Document.DocNumber != documentNumber {
				return ErrInvalidResponse
			}
			document = envelope.Result.Document
		}
		if err := connector.ensureInventoryDocumentElement(ctx, configuration, credential, document, productID, storeID, delta); err != nil {
			if !isAmbiguousWrite(err) {
				return err
			}
			var reconcileErr error
			receipt, reconcileErr = inventoryWriteReconciled(ctx, connector, configuration, credential, storeID, productID, request.Quantity, true)
			return reconcileErr
		}
		if _, conductErr := connector.call(ctx, configuration, credential, "catalog.document.conduct", map[string]any{"id": document.ID}); conductErr != nil {
			if !isAmbiguousWrite(conductErr) {
				return conductErr
			}
			receipt, err = inventoryWriteReconciled(ctx, connector, configuration, credential, storeID, productID, request.Quantity, true)
			return err
		}
		receipt, err = inventoryWriteReconciled(ctx, connector, configuration, credential, storeID, productID, request.Quantity, false)
		return err
	})
	return receipt, err
}
