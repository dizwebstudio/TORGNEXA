package connectors

import (
	"strings"
	"testing"
	"time"
)

func TestFiscalReceiptContractIsExactAndMarkingSafe(t *testing.T) {
	r := FiscalReceiptRequest{
		ExternalID:     "order:1",
		IdempotencyKey: "fiscal:1",
		Kind:           FiscalSale,
		Total:          PaymentAmount{MinorUnits: 12345, Currency: "RUB"},
		Marking: []FiscalMarkingLink{{
			CodeFingerprint:    strings.Repeat("a", 64),
			VerificationStatus: "verified",
		}},
		CreatedAt: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Marking[0].CodeFingerprint = "raw-code"
	if err := r.Validate(); err == nil {
		t.Fatal("raw marking material must not satisfy the fiscal SDK contract")
	}
}

func TestFiscalCorrectionRequiresOriginalReference(t *testing.T) {
	r := FiscalReceiptRequest{ExternalID: "corr:1", IdempotencyKey: "idem:1", Kind: FiscalCorrection, Total: PaymentAmount{MinorUnits: 1, Currency: "RUB"}, CreatedAt: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)}
	if err := r.Validate(); err == nil {
		t.Fatal("correction must reference the original fiscal operation")
	}
}
