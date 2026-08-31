package connectors

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
)

func validPublicationRequest(t *testing.T) ProductPublicationRequest {
	t.Helper()
	return ProductPublicationRequest{
		Operation: marketplacepublication.OperationCreateProduct,
		Snapshot: marketplacepublication.Snapshot{
			ID: "mps_1", Target: marketplacepublication.Target{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ProductID: "product-1", ConnectorAccountID: "account-1", ConnectorID: "ozon", Locale: "ru-RU", Jurisdiction: "RU"},
			Version: 1, SKU: "SKU-1", Title: "Товар", CategoryCode: "category-1", PriceMinor: 100, Currency: "RUB", ProductStatus: "active", CatalogVersion: 1, AssembledAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		},
		IdempotencyKey: "publication-1", DryRun: true, QualityReceiptID: "pqr_1",
	}
}

func TestProductPublicationRequestRequiresApprovalForLiveWrite(t *testing.T) {
	request := validPublicationRequest(t)
	request.DryRun = false
	if request.Validate() == nil {
		t.Fatal("live publication without approval must fail closed")
	}
	request.ApprovalRequestID = "approval-1"
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProductPublicationReceiptRejectsUnexplainedRejection(t *testing.T) {
	receipt := ProductPublicationReceipt{Status: PublicationRejected, ObservedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)}
	if receipt.Validate() == nil {
		t.Fatal("rejection must have a normalized error code")
	}
}
