package uniteconomics

// This file is the second, normalized layer of the Task 167 economics
// package. It deliberately consumes facts, rather than connector payloads or
// ledger rows, so the calculation is replayable and safe to expose in a
// report snapshot.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

const FinancialAlgorithmVersion = "seller-financial-analytics-v1"
const FinancialMetricDefinitionVersion = "seller-financial-metrics-v1"

// FinancialFactKind is the provider-neutral classification of a normalized
// monetary fact. AmountMinor retains the source sign; the report converts
// expenses to positive values for explainable presentation.
type FinancialFactKind string

const (
	FactCommission      FinancialFactKind = "commission"
	FactPaymentFee      FinancialFactKind = "payment_fee"
	FactLogistics       FinancialFactKind = "logistics"
	FactStorage         FinancialFactKind = "storage"
	FactAdvertising     FinancialFactKind = "advertising"
	FactPromotion       FinancialFactKind = "promotion"
	FactPenalty         FinancialFactKind = "penalty"
	FactCompensation    FinancialFactKind = "compensation"
	FactRefund          FinancialFactKind = "refund"
	FactPayout          FinancialFactKind = "payout"
	FactBankReceipt     FinancialFactKind = "bank_receipt"
	FactSupplierPayment FinancialFactKind = "supplier_payment"
	FactTax             FinancialFactKind = "tax"
	FactOther           FinancialFactKind = "other"
)

func (k FinancialFactKind) Valid() bool {
	switch k {
	case FactCommission, FactPaymentFee, FactLogistics, FactStorage, FactAdvertising, FactPromotion,
		FactPenalty, FactCompensation, FactRefund, FactPayout, FactBankReceipt, FactSupplierPayment,
		FactTax, FactOther:
		return true
	default:
		return false
	}
}

// FinancialFact is a small, redacted fact envelope. SourceRef and source
// identifiers are evidence pointers only; raw provider responses and secrets
// never enter the calculation input or its digest.
type FinancialFact struct {
	ID             string            `json:"id"`
	SourceSystem   string            `json:"source_system"`
	SourceAccount  string            `json:"source_account"`
	SourceRef      string            `json:"source_ref"`
	IdempotencyKey string            `json:"idempotency_key"`
	OrderID        string            `json:"order_id,omitempty"`
	OrderItemID    string            `json:"order_item_id,omitempty"`
	SKU            string            `json:"sku,omitempty"`
	ChannelRef     string            `json:"channel_ref,omitempty"`
	WarehouseID    string            `json:"warehouse_id,omitempty"`
	CampaignID     string            `json:"campaign_id,omitempty"`
	Kind           FinancialFactKind `json:"kind"`
	AmountMinor    int64             `json:"amount_minor_units"`
	Currency       string            `json:"currency"`
	Basis          Basis             `json:"basis"`
	OccurredAt     time.Time         `json:"occurred_at"`
	Expected       bool              `json:"expected"`
	Confirmed      bool              `json:"confirmed"`
	Quality        ValueStatus       `json:"value_status"`
	Disputed       bool              `json:"disputed"`
}

// SaleLineFact is an as-of sale line. A nil COGS is intentional and means
// that historical stock valuation is unavailable; it must not be converted to
// zero by a report query.
type SaleLineFact struct {
	ID                string
	OrderID           string
	OrderItemID       string
	SKU               string
	ChannelRef        string
	WarehouseID       string
	Currency          string
	OccurredAt        time.Time
	QuantityMilli     int64
	GrossMinor        int64
	DiscountMinor     int64
	CancellationMinor int64
	RefundMinor       int64
	COGSMinor         *int64
}

// FinancialInput is the bounded input to the deterministic seller report.
type FinancialInput struct {
	OrganizationID    string
	WorkspaceID       string
	Basis             Basis
	From              time.Time
	To                time.Time
	ReportingCurrency string
	SaleLines         []SaleLineFact
	Facts             []FinancialFact
}

// FinancialRow is the explainable P&L result. All monetary amounts are minor
// units in Currency. Status fields distinguish an observed zero from missing
// evidence.
type FinancialRow struct {
	Level                  string        `json:"level"`
	Day                    string        `json:"day,omitempty"`
	ChannelRef             string        `json:"channel_ref"`
	OrderID                string        `json:"order_id,omitempty"`
	SKU                    string        `json:"sku,omitempty"`
	Currency               string        `json:"currency"`
	Basis                  Basis         `json:"basis"`
	Orders                 int64         `json:"orders"`
	UnitsMilli             int64         `json:"units_milli"`
	GrossMinor             int64         `json:"gross_minor_units"`
	DiscountMinor          int64         `json:"discount_minor_units"`
	CancellationMinor      int64         `json:"cancellation_minor_units"`
	RefundMinor            int64         `json:"refund_minor_units"`
	NetSalesMinor          int64         `json:"net_sales_minor_units"`
	COGSMinor              int64         `json:"cogs_minor_units"`
	CommissionMinor        int64         `json:"commission_minor_units"`
	PaymentFeeMinor        int64         `json:"payment_fee_minor_units"`
	LogisticsMinor         int64         `json:"logistics_minor_units"`
	StorageMinor           int64         `json:"storage_minor_units"`
	AdvertisingMinor       int64         `json:"advertising_minor_units"`
	PromotionMinor         int64         `json:"promotion_minor_units"`
	PenaltyMinor           int64         `json:"penalty_minor_units"`
	CompensationMinor      int64         `json:"compensation_minor_units"`
	ContributionMinor      int64         `json:"contribution_profit_minor_units"`
	GrossProfitMinor       int64         `json:"gross_profit_minor_units"`
	MarginBPS              int64         `json:"margin_basis_points"`
	TakeRateBPS            int64         `json:"take_rate_basis_points"`
	RefundRateBPS          int64         `json:"refund_rate_basis_points"`
	LogisticsPerOrderMinor int64         `json:"logistics_per_order_minor_units"`
	COGSStatus             ValueStatus   `json:"cogs_status"`
	CommissionStatus       ValueStatus   `json:"commission_status"`
	LogisticsStatus        ValueStatus   `json:"logistics_status"`
	StorageStatus          ValueStatus   `json:"storage_status"`
	AdvertisingStatus      ValueStatus   `json:"advertising_status"`
	QualityStatus          QualityStatus `json:"quality_status"`
	QualityReasons         []string      `json:"quality_reasons,omitempty"`
	CoveragePercent        int64         `json:"coverage_percent"`
	SourceRefs             []string      `json:"source_refs"`
}

// CashFlowRow is a cash-basis view. Marketplace payout is an inflow; its
// settlement components are not added again as revenue, preventing the common
// payout-plus-settlement double count.
type CashFlowRow struct {
	ChannelRef           string        `json:"channel_ref"`
	Currency             string        `json:"currency"`
	OpeningMinor         int64         `json:"opening_minor_units"`
	PayoutMinor          int64         `json:"payout_minor_units"`
	BankReceiptMinor     int64         `json:"bank_receipt_minor_units"`
	RefundMinor          int64         `json:"refund_minor_units"`
	SupplierPaymentMinor int64         `json:"supplier_payment_minor_units"`
	LogisticsMinor       int64         `json:"logistics_minor_units"`
	AdvertisingMinor     int64         `json:"advertising_minor_units"`
	StorageMinor         int64         `json:"storage_minor_units"`
	FeeMinor             int64         `json:"fee_minor_units"`
	PenaltyMinor         int64         `json:"penalty_minor_units"`
	TaxMinor             int64         `json:"tax_minor_units"`
	OtherMinor           int64         `json:"other_minor_units"`
	NetCashMinor         int64         `json:"net_cash_minor_units"`
	QualityStatus        QualityStatus `json:"quality_status"`
	QualityReasons       []string      `json:"quality_reasons,omitempty"`
	CoveragePercent      int64         `json:"coverage_percent"`
	SourceRefs           []string      `json:"source_refs"`
}

// FinancialSnapshot is immutable report evidence. A recalculation creates a
// new snapshot with a new input digest.
type FinancialSnapshot struct {
	ID                       string         `json:"id"`
	GeneratedAt              time.Time      `json:"generated_at"`
	OrganizationID           string         `json:"organization_id"`
	WorkspaceID              string         `json:"workspace_id"`
	From                     time.Time      `json:"from"`
	To                       time.Time      `json:"to"`
	Basis                    Basis          `json:"basis"`
	ReportingCurrency        string         `json:"reporting_currency,omitempty"`
	AlgorithmVersion         string         `json:"algorithm_version"`
	MetricDefinitionVersion  string         `json:"metric_definition_version"`
	AllocationPolicyVersion  string         `json:"allocation_policy_version"`
	ValuationPolicyVersion   string         `json:"valuation_policy_version"`
	AttributionPolicyVersion string         `json:"attribution_policy_version"`
	InputDigest              string         `json:"input_digest"`
	Rows                     []FinancialRow `json:"rows"`
	DetailRows               []FinancialRow `json:"detail_rows"`
	CashRows                 []CashFlowRow  `json:"cash_rows"`
	QualityStatus            QualityStatus  `json:"quality_status"`
	CoveragePercent          int64          `json:"coverage_percent"`
	QualityReasons           []string       `json:"quality_reasons,omitempty"`
}

type financialAccumulator struct {
	row         financialRowBase
	orders      map[string]struct{}
	refs        map[string]struct{}
	reasons     map[string]struct{}
	has         map[FinancialFactKind]bool
	cogsMissing bool
	disputed    bool
}

type financialRowBase struct {
	FinancialRow
}

func newFinancialAccumulator(level, day, channel, orderID, sku, currency string, basis Basis) *financialAccumulator {
	return &financialAccumulator{row: financialRowBase{FinancialRow: FinancialRow{Level: level, Day: day, ChannelRef: normalizeChannel(channel), OrderID: orderID, SKU: sku, Currency: currency, Basis: basis, COGSStatus: ValueMissing, CommissionStatus: ValueMissing, LogisticsStatus: ValueMissing, StorageStatus: ValueMissing, AdvertisingStatus: ValueMissing}}, orders: map[string]struct{}{}, refs: map[string]struct{}{}, reasons: map[string]struct{}{}, has: map[FinancialFactKind]bool{}}
}

func (a *financialAccumulator) ref(value string) {
	if value != "" {
		a.refs[value] = struct{}{}
	}
}
func (a *financialAccumulator) reason(value string) {
	if value != "" {
		a.reasons[value] = struct{}{}
	}
}

func addFinancial(target *int64, value int64) error { return addChecked(target, value) }

func validateFinancialInput(in FinancialInput) error {
	if in.OrganizationID == "" || in.WorkspaceID == "" || !in.Basis.Valid() || in.From.IsZero() || in.To.IsZero() || !in.To.After(in.From) || in.To.Sub(in.From) > maxPeriod || !in.From.Equal(in.From.UTC()) || !in.To.Equal(in.To.UTC()) {
		return ErrInvalid
	}
	if in.ReportingCurrency != "" && !currencyPattern.MatchString(in.ReportingCurrency) {
		return ErrInvalid
	}
	for _, line := range in.SaleLines {
		if line.ID == "" || line.OrderID == "" || line.SKU == "" || !currencyPattern.MatchString(line.Currency) || line.OccurredAt.IsZero() || !line.OccurredAt.Equal(line.OccurredAt.UTC()) || line.QuantityMilli < 0 || line.GrossMinor < 0 || line.DiscountMinor < 0 || line.CancellationMinor < 0 || line.RefundMinor < 0 || line.DiscountMinor > line.GrossMinor || line.CancellationMinor > line.GrossMinor || line.RefundMinor > line.GrossMinor || (line.COGSMinor != nil && *line.COGSMinor < 0) {
			return ErrInvalid
		}
	}
	for _, fact := range in.Facts {
		if fact.ID == "" || fact.SourceSystem == "" || fact.SourceAccount == "" || fact.SourceRef == "" || fact.IdempotencyKey == "" || !fact.Kind.Valid() || !currencyPattern.MatchString(fact.Currency) || fact.OccurredAt.IsZero() || !fact.OccurredAt.Equal(fact.OccurredAt.UTC()) || (fact.Basis != "" && !fact.Basis.Valid()) {
			return ErrInvalid
		}
	}
	return nil
}

func financialDigest(in FinancialInput) string {
	lines := append([]SaleLineFact(nil), in.SaleLines...)
	sort.Slice(lines, func(i, j int) bool { return lines[i].ID < lines[j].ID })
	facts := append([]FinancialFact(nil), in.Facts...)
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].SourceRef == facts[j].SourceRef {
			return facts[i].ID < facts[j].ID
		}
		return facts[i].SourceRef < facts[j].SourceRef
	})
	wire := struct {
		OrganizationID, WorkspaceID string
		Basis                       Basis
		From, To                    time.Time
		ReportingCurrency           string
		SaleLines                   []SaleLineFact
		Facts                       []FinancialFact
	}{in.OrganizationID, in.WorkspaceID, in.Basis, in.From, in.To, in.ReportingCurrency, lines, facts}
	b, _ := json.Marshal(wire)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func absFinancial(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, ErrInvalid
	}
	if value < 0 {
		return -value, nil
	}
	return value, nil
}

func addLine(a *financialAccumulator, line SaleLineFact) error {
	a.orders[line.OrderID] = struct{}{}
	for target, value := range map[*int64]int64{&a.row.GrossMinor: line.GrossMinor, &a.row.DiscountMinor: line.DiscountMinor, &a.row.CancellationMinor: line.CancellationMinor, &a.row.RefundMinor: line.RefundMinor} {
		if err := addFinancial(target, value); err != nil {
			return err
		}
	}
	if err := addFinancial(&a.row.UnitsMilli, line.QuantityMilli); err != nil {
		return err
	}
	a.ref("sale:" + line.ID)
	if line.COGSMinor == nil {
		a.cogsMissing = true
		a.reason("historical_cogs_unavailable")
	} else if err := addFinancial(&a.row.COGSMinor, *line.COGSMinor); err != nil {
		return err
	}
	return nil
}

func factChannel(f FinancialFact, orders map[string]string) string {
	if strings.TrimSpace(f.ChannelRef) != "" {
		return f.ChannelRef
	}
	if f.OrderID != "" {
		return orders[f.OrderID]
	}
	return UnattributedChannel
}

func addFact(a *financialAccumulator, f FinancialFact) error {
	amount, err := absFinancial(f.AmountMinor)
	if err != nil {
		return err
	}
	a.has[f.Kind] = true
	a.ref("fact:" + f.SourceRef)
	if f.Disputed {
		a.disputed = true
		a.reason("disputed_fact")
	}
	if f.Expected && !f.Confirmed {
		a.reason("estimated_fact")
	}
	var target *int64
	switch f.Kind {
	case FactCommission:
		target = &a.row.CommissionMinor
		a.row.CommissionStatus = ValueObserved
	case FactPaymentFee:
		target = &a.row.PaymentFeeMinor
	case FactLogistics:
		target = &a.row.LogisticsMinor
		a.row.LogisticsStatus = ValueObserved
	case FactStorage:
		target = &a.row.StorageMinor
		a.row.StorageStatus = ValueObserved
	case FactAdvertising:
		target = &a.row.AdvertisingMinor
		a.row.AdvertisingStatus = ValueObserved
	case FactPromotion:
		target = &a.row.PromotionMinor
	case FactPenalty:
		target = &a.row.PenaltyMinor
	case FactCompensation:
		target = &a.row.CompensationMinor
	case FactRefund:
		target = &a.row.RefundMinor
	default:
		return nil
	}
	return addFinancial(target, amount)
}

func finalizeFinancialRow(a *financialAccumulator, reportingCurrency string) FinancialRow {
	r := a.row.FinancialRow
	r.Orders = int64(len(a.orders))
	r.NetSalesMinor = r.GrossMinor - r.DiscountMinor - r.CancellationMinor - r.RefundMinor
	r.GrossProfitMinor = r.NetSalesMinor - r.COGSMinor
	r.ContributionMinor = r.NetSalesMinor - r.COGSMinor - r.CommissionMinor - r.PaymentFeeMinor - r.LogisticsMinor - r.StorageMinor - r.AdvertisingMinor - r.PromotionMinor - r.PenaltyMinor + r.CompensationMinor
	if r.NetSalesMinor > 0 {
		r.MarginBPS = r.ContributionMinor * 10000 / r.NetSalesMinor
		r.TakeRateBPS = (r.CommissionMinor + r.PaymentFeeMinor) * 10000 / r.NetSalesMinor
	}
	if r.GrossMinor > 0 {
		r.RefundRateBPS = r.RefundMinor * 10000 / r.GrossMinor
	}
	if r.Orders > 0 {
		r.LogisticsPerOrderMinor = r.LogisticsMinor / r.Orders
	}
	if !a.cogsMissing {
		r.COGSStatus = ValueObserved
	} else {
		r.COGSStatus = ValueMissing
	}
	if !a.has[FactCommission] {
		a.reason("commission_missing")
	}
	if !a.has[FactLogistics] {
		a.reason("logistics_missing")
	}
	if !a.has[FactStorage] {
		a.reason("storage_missing")
	}
	if !a.has[FactAdvertising] {
		a.reason("advertising_missing")
	}
	if r.ChannelRef == UnattributedChannel {
		a.reason("channel_unresolved")
	}
	if r.ChannelRef == UnattributedChannel && a.has[FactAdvertising] {
		a.reason("advertising_attribution_missing")
	}
	if reportingCurrency != "" && r.Currency != reportingCurrency {
		a.reason("fx_evidence_missing")
	}
	quality := QualityComplete
	if r.ChannelRef == UnattributedChannel {
		quality = QualityUnmatched
	}
	if a.cogsMissing {
		quality = QualityMissingCOGS
	}
	if reportingCurrency != "" && r.Currency != reportingCurrency {
		quality = QualityMissingFX
	}
	if r.ChannelRef == UnattributedChannel && a.has[FactAdvertising] {
		quality = QualityUnattributedAdvertising
	}
	if a.disputed {
		quality = QualityDisputed
	}
	if len(a.reasons) > 0 && quality == QualityComplete {
		quality = QualityPartial
	}
	r.QualityStatus = quality
	r.CoveragePercent = int64(100)
	for _, status := range []ValueStatus{r.COGSStatus, r.CommissionStatus, r.LogisticsStatus, r.StorageStatus, r.AdvertisingStatus} {
		if status == ValueMissing {
			r.CoveragePercent -= 20
		}
	}
	for ref := range a.refs {
		r.SourceRefs = append(r.SourceRefs, ref)
	}
	sort.Strings(r.SourceRefs)
	for reason := range a.reasons {
		r.QualityReasons = append(r.QualityReasons, reason)
	}
	sort.Strings(r.QualityReasons)
	return r
}

type factKey struct{ system, account, ref string }

// CalculateFinancial calculates P&L, detail economics and cash flow from
// normalized facts. It is deterministic, currency-safe and does not treat an
// absent input as zero.
func CalculateFinancial(in FinancialInput, generatedAt time.Time) (FinancialSnapshot, error) {
	if err := validateFinancialInput(in); err != nil || generatedAt.IsZero() || !generatedAt.Equal(generatedAt.UTC()) {
		return FinancialSnapshot{}, ErrInvalid
	}
	digest := financialDigest(in)
	orders := map[string]string{}
	for _, line := range in.SaleLines {
		orders[line.OrderID] = line.ChannelRef
	}
	seen := map[factKey]FinancialFact{}
	facts := make([]FinancialFact, 0, len(in.Facts))
	for _, f := range in.Facts {
		if f.OccurredAt.Before(in.From) || !f.OccurredAt.Before(in.To) {
			continue
		}
		key := factKey{f.SourceSystem, f.SourceAccount, f.SourceRef}
		if old, ok := seen[key]; ok {
			if old.ID != f.ID || old.AmountMinor != f.AmountMinor || old.Currency != f.Currency || old.Kind != f.Kind {
				return FinancialSnapshot{}, ErrConflict
			}
			continue
		}
		seen[key] = f
		facts = append(facts, f)
	}
	channels := map[string]*financialAccumulator{}
	details := map[string]*financialAccumulator{}
	getChannel := func(channel, currency string) *financialAccumulator {
		key := normalizeChannel(channel) + "\x00" + currency
		if channels[key] == nil {
			channels[key] = newFinancialAccumulator("channel", "", channel, "", "", currency, in.Basis)
		}
		return channels[key]
	}
	getDetail := func(line SaleLineFact) *financialAccumulator {
		key := line.OrderID + "\x00" + line.SKU + "\x00" + line.Currency
		if details[key] == nil {
			details[key] = newFinancialAccumulator("order_sku", line.OccurredAt.Format("2006-01-02"), line.ChannelRef, line.OrderID, line.SKU, line.Currency, in.Basis)
		}
		return details[key]
	}
	for _, line := range in.SaleLines {
		if line.OccurredAt.Before(in.From) || !line.OccurredAt.Before(in.To) {
			continue
		}
		if err := addLine(getDetail(line), line); err != nil {
			return FinancialSnapshot{}, err
		}
		if err := addLine(getChannel(line.ChannelRef, line.Currency), line); err != nil {
			return FinancialSnapshot{}, err
		}
	}
	for _, f := range facts {
		channel := factChannel(f, orders)
		ch := getChannel(channel, f.Currency)
		if err := addFact(ch, f); err != nil {
			return FinancialSnapshot{}, err
		}
		if f.OrderID != "" && f.SKU != "" {
			if d := details[f.OrderID+"\x00"+f.SKU+"\x00"+f.Currency]; d != nil {
				if err := addFact(d, f); err != nil {
					return FinancialSnapshot{}, err
				}
			}
		}
	}
	rows := make([]FinancialRow, 0, len(channels))
	for _, a := range channels {
		rows = append(rows, finalizeFinancialRow(a, in.ReportingCurrency))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ChannelRef == rows[j].ChannelRef {
			return rows[i].Currency < rows[j].Currency
		}
		return rows[i].ChannelRef < rows[j].ChannelRef
	})
	detailRows := make([]FinancialRow, 0, len(details))
	for _, a := range details {
		detailRows = append(detailRows, finalizeFinancialRow(a, in.ReportingCurrency))
	}
	sort.Slice(detailRows, func(i, j int) bool {
		if detailRows[i].OrderID == detailRows[j].OrderID {
			return detailRows[i].SKU < detailRows[j].SKU
		}
		return detailRows[i].OrderID < detailRows[j].OrderID
	})
	cash := cashRows(facts, in.From, in.To, orders)
	quality := QualityComplete
	coverage := int64(100)
	reasons := map[string]struct{}{}
	for _, row := range append(append([]FinancialRow{}, rows...), detailRows...) {
		if row.QualityStatus != QualityComplete {
			quality = row.QualityStatus
			for _, reason := range row.QualityReasons {
				reasons[reason] = struct{}{}
			}
		}
		if row.CoveragePercent < coverage {
			coverage = row.CoveragePercent
		}
	}
	for _, row := range cash {
		if row.QualityStatus != QualityComplete && quality == QualityComplete {
			quality = row.QualityStatus
		}
		if row.CoveragePercent < coverage {
			coverage = row.CoveragePercent
		}
		for _, reason := range row.QualityReasons {
			reasons[reason] = struct{}{}
		}
	}
	qualityReasons := make([]string, 0, len(reasons))
	for reason := range reasons {
		qualityReasons = append(qualityReasons, reason)
	}
	sort.Strings(qualityReasons)
	idHash := sha256.Sum256([]byte(digest + string(in.Basis)))
	return FinancialSnapshot{ID: "fin-" + hex.EncodeToString(idHash[:])[:24], GeneratedAt: generatedAt, OrganizationID: in.OrganizationID, WorkspaceID: in.WorkspaceID, From: in.From, To: in.To, Basis: in.Basis, ReportingCurrency: in.ReportingCurrency, AlgorithmVersion: FinancialAlgorithmVersion, MetricDefinitionVersion: FinancialMetricDefinitionVersion, AllocationPolicyVersion: AllocationPolicyVersion, ValuationPolicyVersion: ValuationPolicyVersion, AttributionPolicyVersion: AttributionPolicyVersion, InputDigest: digest, Rows: rows, DetailRows: detailRows, CashRows: cash, QualityStatus: quality, CoveragePercent: coverage, QualityReasons: qualityReasons}, nil
}

func cashRows(facts []FinancialFact, from, to time.Time, orders map[string]string) []CashFlowRow {
	type cashAccumulator struct {
		row           CashFlowRow
		refs, reasons map[string]struct{}
		seen          bool
		disputed      bool
	}
	rows := map[string]*cashAccumulator{}
	get := func(channel, currency string) *cashAccumulator {
		key := normalizeChannel(channel) + "\x00" + currency
		if rows[key] == nil {
			rows[key] = &cashAccumulator{row: CashFlowRow{ChannelRef: normalizeChannel(channel), Currency: currency, QualityStatus: QualityComplete}, refs: map[string]struct{}{}, reasons: map[string]struct{}{}}
		}
		return rows[key]
	}
	for _, f := range facts {
		channel := factChannel(f, orders)
		a := get(channel, f.Currency)
		value, _ := absFinancial(f.AmountMinor)
		a.refs["fact:"+f.SourceRef] = struct{}{}
		a.seen = true
		if f.Disputed {
			a.disputed = true
			a.reasons["disputed_fact"] = struct{}{}
		}
		if f.Expected && !f.Confirmed {
			a.reasons["estimated_fact"] = struct{}{}
		}
		var target *int64
		switch f.Kind {
		case FactPayout:
			target = &a.row.PayoutMinor
		case FactBankReceipt:
			target = &a.row.BankReceiptMinor
		case FactRefund:
			target = &a.row.RefundMinor
		case FactSupplierPayment:
			target = &a.row.SupplierPaymentMinor
		case FactLogistics:
			target = &a.row.LogisticsMinor
		case FactAdvertising:
			target = &a.row.AdvertisingMinor
		case FactStorage:
			target = &a.row.StorageMinor
		case FactCommission, FactPaymentFee:
			target = &a.row.FeeMinor
		case FactPenalty:
			target = &a.row.PenaltyMinor
		case FactTax:
			target = &a.row.TaxMinor
		case FactOther:
			target = &a.row.OtherMinor
		}
		if target != nil {
			_ = addFinancial(target, value)
		}
	}
	out := make([]CashFlowRow, 0, len(rows))
	for _, a := range rows {
		a.row.NetCashMinor = a.row.OpeningMinor + a.row.PayoutMinor + a.row.BankReceiptMinor - a.row.RefundMinor - a.row.SupplierPaymentMinor - a.row.LogisticsMinor - a.row.AdvertisingMinor - a.row.StorageMinor - a.row.FeeMinor - a.row.PenaltyMinor - a.row.TaxMinor - a.row.OtherMinor
		if a.row.ChannelRef == UnattributedChannel {
			a.reasons["cash_source_unmatched"] = struct{}{}
		}
		if a.disputed {
			a.row.QualityStatus = QualityDisputed
		}
		if len(a.reasons) > 0 && a.row.QualityStatus == QualityComplete {
			a.row.QualityStatus = QualityPartial
		}
		a.row.CoveragePercent = 100
		if a.row.PayoutMinor == 0 && a.row.BankReceiptMinor == 0 {
			a.row.CoveragePercent = 50
			a.reasons["cash_receipt_missing"] = struct{}{}
		}
		for ref := range a.refs {
			a.row.SourceRefs = append(a.row.SourceRefs, ref)
		}
		sort.Strings(a.row.SourceRefs)
		for reason := range a.reasons {
			a.row.QualityReasons = append(a.row.QualityReasons, reason)
		}
		sort.Strings(a.row.QualityReasons)
		out = append(out, a.row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelRef == out[j].ChannelRef {
			return out[i].Currency < out[j].Currency
		}
		return out[i].ChannelRef < out[j].ChannelRef
	})
	return out
}
