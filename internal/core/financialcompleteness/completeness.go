// Package financialcompleteness describes the evidence required to call a
// seller-finance report complete. It contains no provider or storage code.
package financialcompleteness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var (
	ErrInvalid      = errors.New("financial completeness: invalid value")
	ErrConflict     = errors.New("financial completeness: conflicting source evidence")
	refPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Basis is the report recognition basis.
type Basis string

const (
	BasisOrderAccrual Basis = "order_accrual"
	BasisSettlement   Basis = "settlement"
	BasisCash         Basis = "cash"
)

func (b Basis) Valid() bool { return b == BasisOrderAccrual || b == BasisSettlement || b == BasisCash }

// SourceKind is an authoritative or supporting finance evidence class.
type SourceKind string

const (
	SourceOrder       SourceKind = "order"
	SourcePayment     SourceKind = "payment"
	SourceRefund      SourceKind = "refund"
	SourcePayout      SourceKind = "payout"
	SourceBankReceipt SourceKind = "bank_receipt"
	SourceCOGS        SourceKind = "cogs"
	SourceFX          SourceKind = "fx"
	SourceAdvertising SourceKind = "advertising"
	SourcePromotion   SourceKind = "promotion"
	SourceSettlement  SourceKind = "settlement"
	SourceOther       SourceKind = "other"
)

func (k SourceKind) Valid() bool {
	switch k {
	case SourceOrder, SourcePayment, SourceRefund, SourcePayout, SourceBankReceipt, SourceCOGS, SourceFX, SourceAdvertising, SourcePromotion, SourceSettlement, SourceOther:
		return true
	default:
		return false
	}
}

// Quality is the evidence quality, not a monetary value.
type Quality string

const (
	QualityObserved  Quality = "observed"
	QualityConfirmed Quality = "confirmed"
	QualityEstimated Quality = "estimated"
	QualityMissing   Quality = "missing"
	QualityUnmatched Quality = "unmatched"
	QualityStale     Quality = "stale"
	QualityDisputed  Quality = "disputed"
	QualityConflict  Quality = "conflict"
)

func (q Quality) Valid() bool {
	switch q {
	case QualityObserved, QualityConfirmed, QualityEstimated, QualityMissing, QualityUnmatched, QualityStale, QualityDisputed, QualityConflict:
		return true
	default:
		return false
	}
}

// EvaluationStatus is the report-level completeness state.
type EvaluationStatus string

const (
	EvaluationComplete  EvaluationStatus = "complete"
	EvaluationPartial   EvaluationStatus = "partial"
	EvaluationStale     EvaluationStatus = "stale"
	EvaluationUnmatched EvaluationStatus = "unmatched"
	EvaluationDisputed  EvaluationStatus = "disputed"
	EvaluationConflict  EvaluationStatus = "conflict"
)

// SourceRecord is a redacted append-only source fact. AccountRef must be a
// masked/provider reference; full bank details and credentials never belong
// in this structure.
type SourceRecord struct {
	ID                string     `json:"id"`
	Kind              SourceKind `json:"kind"`
	SourceSystem      string     `json:"source_system"`
	AccountRef        string     `json:"account_ref"`
	SourceRef         string     `json:"source_ref"`
	StatementID       string     `json:"statement_id,omitempty"`
	OrderID           string     `json:"order_id,omitempty"`
	PayoutID          string     `json:"payout_id,omitempty"`
	SKU               string     `json:"sku,omitempty"`
	CampaignID        string     `json:"campaign_id,omitempty"`
	AttributionStatus string     `json:"attribution_status,omitempty"`
	AmountMinor       int64      `json:"amount_minor_units"`
	Currency          string     `json:"currency"`
	State             string     `json:"state"`
	Quality           Quality    `json:"quality"`
	OccurredAt        time.Time  `json:"occurred_at"`
	PostedAt          time.Time  `json:"posted_at,omitempty"`
	SourceDigest      string     `json:"source_digest"`
	CreatedAt         time.Time  `json:"created_at"`
}

// Validate rejects raw-looking bank identifiers and non-redacted evidence.
func (r SourceRecord) Validate() error {
	if !refPattern.MatchString(r.ID) || !r.Kind.Valid() || !refPattern.MatchString(r.SourceSystem) || r.AccountRef == "" || len(r.AccountRef) > 128 || !refPattern.MatchString(r.SourceRef) || !currencyPattern.MatchString(r.Currency) || !qualityState(r.State) || !r.Quality.Valid() || r.OccurredAt.IsZero() || !utc(r.OccurredAt) || (!r.PostedAt.IsZero() && !utc(r.PostedAt)) || !digestPattern.MatchString(r.SourceDigest) || (!r.CreatedAt.IsZero() && !utc(r.CreatedAt)) {
		return ErrInvalid
	}
	if digitsOnly(r.AccountRef) && len(r.AccountRef) >= 12 {
		return ErrInvalid
	}
	for _, value := range []string{r.StatementID, r.OrderID, r.PayoutID, r.SKU, r.CampaignID} {
		if value != "" && len(value) > 192 {
			return ErrInvalid
		}
	}
	if r.AttributionStatus != "" && !refPattern.MatchString(r.AttributionStatus) {
		return ErrInvalid
	}
	return nil
}

func qualityState(value string) bool {
	switch value {
	case "pending", "posted", "reversed", "fee", "transfer", "unknown", "matched", "unmatched", "disputed", "observed":
		return true
	default:
		return false
	}
}

func digitsOnly(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func utc(value time.Time) bool { return value.Equal(value.UTC()) }

// Deduplicate keeps identical source evidence once and rejects a conflicting
// reuse of the same provider identity.
func Deduplicate(records []SourceRecord) ([]SourceRecord, error) {
	seen := make(map[string]SourceRecord, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return nil, err
		}
		key := string(record.Kind) + "\x00" + record.SourceSystem + "\x00" + record.AccountRef + "\x00" + record.SourceRef
		if previous, ok := seen[key]; ok {
			if previous.AmountMinor != record.AmountMinor || previous.Currency != record.Currency || previous.SourceDigest != record.SourceDigest {
				return nil, ErrConflict
			}
			continue
		}
		seen[key] = record
	}
	out := make([]SourceRecord, 0, len(seen))
	for _, record := range seen {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Requirement is the reviewable source matrix row.
type Requirement struct {
	Code            string     `json:"code"`
	Label           string     `json:"label"`
	CanonicalSource SourceKind `json:"canonical_source"`
	Fallback        string     `json:"fallback"`
	RequiredFor     []Basis    `json:"required_for"`
	Retention       string     `json:"retention"`
}

// Matrix returns the canonical metric-to-source matrix in stable order.
func Matrix() []Requirement {
	return []Requirement{
		{Code: "revenue", Label: "Продажи", CanonicalSource: SourceOrder, Fallback: "none", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "refunds", Label: "Возвраты и refunds", CanonicalSource: SourceRefund, Fallback: "payment_or_settlement", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "payout", Label: "Выплаты площадок и эквайринга", CanonicalSource: SourcePayout, Fallback: "settlement_entry", RequiredFor: []Basis{BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "cash", Label: "Поступления банка", CanonicalSource: SourceBankReceipt, Fallback: "acquirer_receipt", RequiredFor: []Basis{BasisCash}, Retention: "bank_evidence"},
		{Code: "cogs", Label: "Историческая себестоимость", CanonicalSource: SourceCOGS, Fallback: "wms_or_receiving_layer", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "fx", Label: "Курсы и conversion snapshots", CanonicalSource: SourceFX, Fallback: "none", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "advertising", Label: "Реклама", CanonicalSource: SourceAdvertising, Fallback: "unattributed_channel_spend", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
		{Code: "promotion", Label: "Промо и субсидии", CanonicalSource: SourcePromotion, Fallback: "settlement_adjustment", RequiredFor: []Basis{BasisOrderAccrual, BasisSettlement, BasisCash}, Retention: "financial_history"},
	}
}

// Component is one evaluated completeness component.
type Component struct {
	Code          string  `json:"code"`
	Label         string  `json:"label"`
	Quality       Quality `json:"quality"`
	Required      bool    `json:"required"`
	EvidenceCount int     `json:"evidence_count"`
	AmountMinor   int64   `json:"amount_minor_units"`
	Currency      string  `json:"currency"`
	Reason        string  `json:"reason,omitempty"`
}

// Evaluation is a deterministic, non-zero-filling completeness result.
type Evaluation struct {
	Basis             Basis            `json:"basis"`
	From              time.Time        `json:"from"`
	To                time.Time        `json:"to"`
	ReportingCurrency string           `json:"reporting_currency,omitempty"`
	Status            EvaluationStatus `json:"status"`
	CoveragePercent   int              `json:"coverage_percent"`
	Components        []Component      `json:"components"`
	MissingCodes      []string         `json:"missing_codes,omitempty"`
	Warnings          []string         `json:"warnings,omitempty"`
	SourceRefs        []string         `json:"source_refs"`
}

// Evaluate applies source precedence and records gaps without inventing a
// value. FX is required only when evidence contains a foreign currency.
func Evaluate(basis Basis, from, to time.Time, reportingCurrency string, records []SourceRecord) (Evaluation, error) {
	if !basis.Valid() || from.IsZero() || to.IsZero() || !to.After(from) || !utc(from) || !utc(to) || (reportingCurrency != "" && !currencyPattern.MatchString(reportingCurrency)) {
		return Evaluation{}, ErrInvalid
	}
	unique, err := Deduplicate(records)
	if err != nil {
		return Evaluation{}, err
	}
	byKind := make(map[SourceKind][]SourceRecord)
	foreign := false
	refs := make([]string, 0, len(unique))
	for _, record := range unique {
		if record.OccurredAt.Before(from) || !record.OccurredAt.Before(to) {
			continue
		}
		byKind[record.Kind] = append(byKind[record.Kind], record)
		if reportingCurrency != "" && record.Currency != reportingCurrency {
			foreign = true
		}
		refs = append(refs, record.SourceRef)
	}
	if !sort.StringsAreSorted(refs) {
		sort.Strings(refs)
	}
	components := make([]Component, 0, len(Matrix()))
	missing := make([]string, 0)
	warnings := make([]string, 0)
	requiredCount, goodCount := 0, 0
	for _, requirement := range Matrix() {
		required := false
		for _, candidate := range requirement.RequiredFor {
			if candidate == basis {
				required = true
			}
		}
		if requirement.Code == "fx" && !foreign {
			required = false
		}
		items := byKind[requirement.CanonicalSource]
		component := Component{Code: requirement.Code, Label: requirement.Label, Required: required, EvidenceCount: len(items), Currency: reportingCurrency}
		if len(items) > 0 {
			component.Quality = QualityObserved
			for _, item := range items {
				component.AmountMinor += item.AmountMinor
				if component.Currency == "" {
					component.Currency = item.Currency
				}
				if item.Quality == QualityConflict || item.Quality == QualityDisputed {
					component.Quality = item.Quality
				}
				if item.Quality == QualityStale && component.Quality == QualityObserved {
					component.Quality = QualityStale
				}
			}
		} else {
			component.Quality = QualityMissing
			component.Reason = "source_not_observed"
		}
		if required {
			requiredCount++
			if component.Quality == QualityObserved || component.Quality == QualityConfirmed {
				goodCount++
			} else {
				missing = append(missing, requirement.Code)
			}
		}
		if !required && component.Quality == QualityMissing {
			warnings = append(warnings, requirement.Code+":optional_source_missing")
		}
		components = append(components, component)
	}
	coverage := 100
	if requiredCount > 0 {
		coverage = goodCount * 100 / requiredCount
	}
	status := EvaluationComplete
	if len(missing) > 0 {
		status = EvaluationPartial
	}
	for _, component := range components {
		if !component.Required {
			continue
		}
		if component.Quality == QualityConflict || component.Quality == QualityDisputed {
			if component.Quality == QualityConflict {
				status = EvaluationConflict
			} else {
				status = EvaluationDisputed
			}
			break
		}
		if component.Quality == QualityStale && status != EvaluationConflict && status != EvaluationDisputed {
			status = EvaluationStale
		}
		if component.Quality == QualityUnmatched && status != EvaluationConflict && status != EvaluationDisputed && status != EvaluationStale {
			status = EvaluationUnmatched
		}
	}
	return Evaluation{Basis: basis, From: from, To: to, ReportingCurrency: reportingCurrency, Status: status, CoveragePercent: coverage, Components: components, MissingCodes: missing, Warnings: warnings, SourceRefs: refs}, nil
}

// Digest returns a stable digest for redacted source evidence and is suitable
// for idempotency/audit metadata.
func Digest(record SourceRecord) string {
	value := record.Kind.String() + "\x00" + record.SourceSystem + "\x00" + record.AccountRef + "\x00" + record.SourceRef + "\x00" + stringAmount(record.AmountMinor) + "\x00" + record.Currency
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func stringAmount(value int64) string { return strconv.FormatInt(value, 10) }

func (k SourceKind) String() string { return string(k) }

// Finding is a tenant-scoped reconciliation exception projection.
type Finding struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	SubjectRef    string    `json:"subject_ref"`
	ExpectedMinor int64     `json:"expected_minor_units"`
	ObservedMinor int64     `json:"observed_minor_units"`
	Currency      string    `json:"currency"`
	Severity      string    `json:"severity"`
	Status        string    `json:"status"`
	Explanation   string    `json:"explanation"`
	OwnerRef      string    `json:"owner_ref,omitempty"`
	DetectedAt    time.Time `json:"detected_at"`
	ResolvedAt    time.Time `json:"resolved_at,omitempty"`
}

// Validate checks an exception before persistence.
func (f Finding) Validate() error {
	if !refPattern.MatchString(f.ID) || !refPattern.MatchString(f.Kind) || (f.SubjectRef != "" && !refPattern.MatchString(f.SubjectRef)) || !currencyPattern.MatchString(f.Currency) || f.Severity == "" || f.Status == "" || len(f.Explanation) == 0 || len(f.Explanation) > 500 || f.DetectedAt.IsZero() || !utc(f.DetectedAt) || (!f.ResolvedAt.IsZero() && !utc(f.ResolvedAt)) {
		return ErrInvalid
	}
	return nil
}
