package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

type financialExportAuditRepository struct {
	record audit.Record
}

func (r *financialExportAuditRepository) Append(_ context.Context, _ tenancy.Scope, record audit.Record) error {
	r.record = record
	return nil
}

func TestFinancialExportIsAuditedBeforeDelivery(t *testing.T) {
	scope, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	repository := &financialExportAuditRepository{}
	auditor, err := audit.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/reports/seller_profit_and_loss?format=csv&run_id=finrun-1", nil)
	identity := Principal{Issuer: "issuer", Subject: "subject", SubjectRef: "subject-ref"}
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityKey{}, identity))
	request.Header.Set("X-Request-ID", "request-1")
	if !auditFinancialExport(request, scope, auditor, "seller_profit_and_loss", "csv", 3) {
		t.Fatal("financial export audit failed")
	}
	if repository.record.Action != "financial_report.export" || repository.record.ResourceID != "seller_profit_and_loss" || repository.record.CorrelationID != "request-1" || repository.record.Risk != audit.RiskRead {
		t.Fatalf("unexpected audit record: %#v", repository.record)
	}
	if repository.record.CreatedAt.IsZero() || repository.record.CreatedAt.After(time.Now().UTC()) {
		t.Fatalf("unexpected audit timestamp: %v", repository.record.CreatedAt)
	}
}
