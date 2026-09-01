package financialcompleteness

import (
	"errors"
	"testing"
	"time"
)

var completenessTestTime = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

func completenessRecord(id string, kind SourceKind, amount int64, currency string) SourceRecord {
	return SourceRecord{ID: id, Kind: kind, SourceSystem: "fixture", AccountRef: "account-*42", SourceRef: "source-" + id, AmountMinor: amount, Currency: currency, State: "posted", Quality: QualityConfirmed, OccurredAt: completenessTestTime.Add(time.Hour), SourceDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
}

func TestEvaluateDoesNotZeroFillMissingSources(t *testing.T) {
	records := []SourceRecord{
		completenessRecord("order", SourceOrder, 10000, "RUB"),
		completenessRecord("refund", SourceRefund, 0, "RUB"),
		completenessRecord("payout", SourcePayout, 8500, "RUB"),
		completenessRecord("bank", SourceBankReceipt, 8500, "RUB"),
		completenessRecord("cogs", SourceCOGS, 4000, "RUB"),
		completenessRecord("ads", SourceAdvertising, 300, "RUB"),
		completenessRecord("promo", SourcePromotion, 100, "RUB"),
	}
	evaluation, err := Evaluate(BasisCash, completenessTestTime, completenessTestTime.Add(24*time.Hour), "RUB", records)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Status != EvaluationComplete || evaluation.CoveragePercent != 100 {
		t.Fatalf("unexpected complete evaluation: %+v", evaluation)
	}

	records = records[:len(records)-1]
	evaluation, err = Evaluate(BasisCash, completenessTestTime, completenessTestTime.Add(24*time.Hour), "RUB", records)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Status != EvaluationPartial || evaluation.CoveragePercent >= 100 {
		t.Fatalf("missing source was hidden: %+v", evaluation)
	}
	if len(evaluation.MissingCodes) == 0 || evaluation.MissingCodes[len(evaluation.MissingCodes)-1] != "promotion" {
		t.Fatalf("promotion gap not reported: %+v", evaluation.MissingCodes)
	}
}

func TestEvaluateRequiresFXOnlyForForeignEvidence(t *testing.T) {
	records := []SourceRecord{completenessRecord("order", SourceOrder, 100, "USD")}
	evaluation, err := Evaluate(BasisOrderAccrual, completenessTestTime, completenessTestTime.Add(24*time.Hour), "RUB", records)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Status != EvaluationPartial {
		t.Fatalf("unexpected status: %+v", evaluation)
	}
	foundFX := false
	for _, code := range evaluation.MissingCodes {
		if code == "fx" {
			foundFX = true
		}
	}
	if !foundFX {
		t.Fatalf("missing FX was not reported: %+v", evaluation.MissingCodes)
	}
}

func TestDeduplicateRejectsConflictingSourceIdentity(t *testing.T) {
	first := completenessRecord("one", SourcePayout, 100, "RUB")
	second := first
	second.ID = "two"
	second.AmountMinor = 101
	if _, err := Deduplicate([]SourceRecord{first, second}); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestSourceRecordRejectsFullAccountNumber(t *testing.T) {
	record := completenessRecord("bank", SourceBankReceipt, 100, "RUB")
	record.AccountRef = "40817810000000000001"
	if !errors.Is(record.Validate(), ErrInvalid) {
		t.Fatal("full account number accepted")
	}
}
