// Package uniteconomics calculates provider-neutral, factual contribution
// economics. It is deliberately pure: canonical orders, payments,
// settlements, returns, costs and FX facts are supplied by adapters and remain
// authoritative outside this package.
package uniteconomics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	AlgorithmVersion         = "channel-unit-economics-v1"
	MetricDefinitionVersion  = "channel-unit-economics-metrics-v1"
	AllocationPolicyVersion  = "allocation-v1"
	ValuationPolicyVersion   = "valuation-v1"
	AttributionPolicyVersion = "attribution-v1"
	UnattributedChannel      = "unattributed"
	maxPeriod                = 366 * 24 * time.Hour
)

var (
	ErrInvalid       = errors.New("unit economics: invalid input")
	ErrMixedCurrency = errors.New("unit economics: mixed currency")
	ErrConflict      = errors.New("unit economics: conflicting source fact")
	channelPattern   = regexp.MustCompile(`^[a-z][a-z0-9._:/-]{0,191}$`)
	currencyPattern  = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Basis selects the date on which a fact is recognised. A calculation never
// mixes bases.
type Basis string

const (
	BasisOrderAccrual Basis = "order_accrual"
	BasisSettlement   Basis = "settlement"
	BasisCash         Basis = "cash"
)

func (b Basis) Valid() bool { return b == BasisOrderAccrual || b == BasisSettlement || b == BasisCash }

// QualityStatus describes whether a row is safe to use as a complete fact.
type QualityStatus string

const (
	QualityComplete                QualityStatus = "complete"
	QualityPartial                 QualityStatus = "partial"
	QualityStale                   QualityStatus = "stale"
	QualityUnmatched               QualityStatus = "unmatched"
	QualityConflict                QualityStatus = "conflict"
	QualityMixedCurrency           QualityStatus = "mixed_currency"
	QualityUnsupported             QualityStatus = "unsupported"
	QualityMissingCOGS             QualityStatus = "missing_cogs"
	QualityMissingFX               QualityStatus = "missing_fx"
	QualityUnmatchedSettlement     QualityStatus = "unmatched_settlement"
	QualityUnattributedAdvertising QualityStatus = "unattributed_advertising"
	QualityDisputed                QualityStatus = "disputed"
)

// ValueStatus distinguishes a measured zero from an unavailable component.
type ValueStatus string

const (
	ValueObserved      ValueStatus = "observed"
	ValueEstimated     ValueStatus = "estimated"
	ValueMissing       ValueStatus = "missing"
	ValueNotApplicable ValueStatus = "not_applicable"
	ValueDisputed      ValueStatus = "disputed"
)

// Component is one exact monetary component. Amount is never interpreted as
// a float and Status prevents an absent fact from being mistaken for zero.
type Component struct {
	MinorUnits int64       `json:"minor_units"`
	Currency   string      `json:"currency"`
	Status     ValueStatus `json:"value_status"`
	ReasonCode string      `json:"reason_code,omitempty"`
	SourceRefs []string    `json:"source_refs,omitempty"`
}

func (c Component) validate() error {
	if !currencyPattern.MatchString(c.Currency) || c.Status == "" || len(c.SourceRefs) > 64 {
		return ErrInvalid
	}
	for _, ref := range c.SourceRefs {
		if ref == "" || len(ref) > 192 {
			return ErrInvalid
		}
	}
	return nil
}

// OrderFact is a normalized immutable order snapshot. Gross is merchandise
// value before discounts; all amounts use the order currency.
type OrderFact struct {
	ID            string    `json:"id"`
	ChannelRef    string    `json:"channel_ref"`
	Currency      string    `json:"currency"`
	OccurredAt    time.Time `json:"occurred_at"`
	QuantityMilli int64     `json:"quantity_milli"`
	GrossMinor    int64     `json:"gross_minor_units"`
	DiscountMinor int64     `json:"discount_minor_units"`
	TaxMinor      int64     `json:"tax_minor_units"`
	ShippingMinor int64     `json:"shipping_minor_units"`
	GrandMinor    int64     `json:"grand_minor_units"`
	RefundMinor   int64     `json:"refund_minor_units"`
	COGSMinor     *int64    `json:"cogs_minor_units,omitempty"`
	Status        string    `json:"status"`
}

// SettlementFact is a normalized append-only ledger entry. Amount retains the
// source sign; the calculation converts expenses to positive component values.
type SettlementFact struct {
	ID            string    `json:"id"`
	SourceSystem  string    `json:"source_system"`
	SourceAccount string    `json:"source_account"`
	EntryRef      string    `json:"entry_ref"`
	OrderID       string    `json:"order_id"`
	ChannelRef    string    `json:"channel_ref"`
	Kind          string    `json:"kind"`
	AmountMinor   int64     `json:"amount_minor_units"`
	Currency      string    `json:"currency"`
	OccurredAt    time.Time `json:"occurred_at"`
	Disputed      bool      `json:"disputed"`
}

// PaymentFact contains a payment processing fee. Settlement fee facts take
// precedence when both sources describe the same order/fee.
type PaymentFact struct {
	ID         string    `json:"id"`
	OrderID    string    `json:"order_id"`
	Currency   string    `json:"currency"`
	FeeMinor   int64     `json:"fee_minor_units"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Input is a bounded calculation request. Facts are copied and sorted before
// hashing, making replay and retry deterministic.
type Input struct {
	OrganizationID    string
	WorkspaceID       string
	Basis             Basis
	From              time.Time
	To                time.Time
	ReportingCurrency string
	Orders            []OrderFact
	Settlements       []SettlementFact
	Payments          []PaymentFact
}

// Row is the explainable channel-level result returned to reports and exports.
type Row struct {
	ChannelRef        string        `json:"channel_ref"`
	Currency          string        `json:"currency"`
	Basis             Basis         `json:"basis"`
	Orders            int64         `json:"orders"`
	UnitsMilli        int64         `json:"units_milli"`
	Gross             Component     `json:"gross_merchandise_value"`
	Discounts         Component     `json:"discounts"`
	Cancellations     Component     `json:"cancellations"`
	Refunds           Component     `json:"refunds_and_returns"`
	NetRevenue        Component     `json:"net_revenue"`
	Commission        Component     `json:"marketplace_commission"`
	PaymentFee        Component     `json:"payment_processing_fee"`
	Logistics         Component     `json:"fulfilment_and_delivery"`
	Storage           Component     `json:"storage"`
	Advertising       Component     `json:"advertising_spend"`
	Promotion         Component     `json:"promotion_subsidy"`
	COGS              Component     `json:"cogs"`
	Penalties         Component     `json:"penalties"`
	Compensation      Component     `json:"compensation"`
	Payout            Component     `json:"payout"`
	Contribution      Component     `json:"contribution_profit"`
	MarginBasisPoints int64         `json:"contribution_margin_bps"`
	CoveragePercent   int64         `json:"coverage_percent"`
	QualityStatus     QualityStatus `json:"quality_status"`
	QualityReasons    []string      `json:"quality_reasons,omitempty"`
	SourceRefs        []string      `json:"source_refs"`
	InputDigest       string        `json:"input_digest"`
}

// Snapshot is immutable for a given input digest. Recalculation creates a new
// snapshot rather than mutating an earlier result.
type Snapshot struct {
	ID                       string        `json:"id"`
	GeneratedAt              time.Time     `json:"generated_at"`
	OrganizationID           string        `json:"organization_id"`
	WorkspaceID              string        `json:"workspace_id"`
	From                     time.Time     `json:"from"`
	To                       time.Time     `json:"to"`
	Basis                    Basis         `json:"basis"`
	ReportingCurrency        string        `json:"reporting_currency,omitempty"`
	AlgorithmVersion         string        `json:"algorithm_version"`
	MetricDefinitionVersion  string        `json:"metric_definition_version"`
	AllocationPolicyVersion  string        `json:"allocation_policy_version"`
	ValuationPolicyVersion   string        `json:"valuation_policy_version"`
	AttributionPolicyVersion string        `json:"attribution_policy_version"`
	InputDigest              string        `json:"input_digest"`
	Rows                     []Row         `json:"rows"`
	QualityStatus            QualityStatus `json:"quality_status"`
}

func normalizeChannel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || !channelPattern.MatchString(value) {
		return UnattributedChannel
	}
	return value
}

func absAmount(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, ErrInvalid
	}
	if value < 0 {
		return -value, nil
	}
	return value, nil
}

func addChecked(target *int64, value int64) error {
	if value > 0 && *target > math.MaxInt64-value || value < 0 && *target < math.MinInt64-value {
		return ErrInvalid
	}
	*target += value
	return nil
}

func component(minor int64, currency string, status ValueStatus, reason string, refs []string) Component {
	return Component{MinorUnits: minor, Currency: currency, Status: status, ReasonCode: reason, SourceRefs: append([]string(nil), refs...)}
}

type rowAccumulator struct {
	row             Row
	refs            map[string]struct{}
	reasons         map[string]struct{}
	hasCOGS         bool
	hasDispute      bool
	conflict        bool
	hasCommission   bool
	hasRefunds      bool
	hasPaymentFee   bool
	hasPayout       bool
	hasPenalty      bool
	hasCompensation bool
}

func (a *rowAccumulator) addRef(ref string) {
	if ref != "" {
		a.refs[ref] = struct{}{}
	}
}
func (a *rowAccumulator) reason(reason string) {
	if reason != "" {
		a.reasons[reason] = struct{}{}
	}
}

func validateInput(in Input) error {
	if in.OrganizationID == "" || in.WorkspaceID == "" || !in.Basis.Valid() || in.From.IsZero() || in.To.IsZero() || !in.To.After(in.From) || in.To.Sub(in.From) > maxPeriod {
		return ErrInvalid
	}
	if !in.From.Equal(in.From.UTC()) || !in.To.Equal(in.To.UTC()) {
		return ErrInvalid
	}
	if in.ReportingCurrency != "" && !currencyPattern.MatchString(in.ReportingCurrency) {
		return ErrInvalid
	}
	for _, order := range in.Orders {
		if order.ID == "" || !currencyPattern.MatchString(order.Currency) || order.OccurredAt.IsZero() || !order.OccurredAt.Equal(order.OccurredAt.UTC()) || order.QuantityMilli < 0 || order.GrossMinor < 0 || order.DiscountMinor < 0 || order.TaxMinor < 0 || order.ShippingMinor < 0 || order.GrandMinor < 0 || order.RefundMinor < 0 || order.DiscountMinor > order.GrossMinor || order.Status == "" {
			return ErrInvalid
		}
		if order.COGSMinor != nil && *order.COGSMinor < 0 {
			return ErrInvalid
		}
	}
	for _, fact := range in.Settlements {
		if fact.ID == "" || fact.SourceSystem == "" || fact.SourceAccount == "" || fact.EntryRef == "" || !currencyPattern.MatchString(fact.Currency) || fact.OccurredAt.IsZero() || !fact.OccurredAt.Equal(fact.OccurredAt.UTC()) || fact.Kind == "" {
			return ErrInvalid
		}
	}
	for _, fact := range in.Payments {
		if fact.ID == "" || fact.OrderID == "" || !currencyPattern.MatchString(fact.Currency) || fact.FeeMinor < 0 || fact.OccurredAt.IsZero() || !fact.OccurredAt.Equal(fact.OccurredAt.UTC()) {
			return ErrInvalid
		}
	}
	return nil
}

func digestInput(in Input) (string, error) {
	orders := append([]OrderFact(nil), in.Orders...)
	sort.Slice(orders, func(i, j int) bool { return orders[i].ID < orders[j].ID })
	settlements := append([]SettlementFact(nil), in.Settlements...)
	sort.Slice(settlements, func(i, j int) bool {
		if settlements[i].EntryRef == settlements[j].EntryRef {
			return settlements[i].ID < settlements[j].ID
		}
		return settlements[i].EntryRef < settlements[j].EntryRef
	})
	payments := append([]PaymentFact(nil), in.Payments...)
	sort.Slice(payments, func(i, j int) bool { return payments[i].ID < payments[j].ID })
	wire := struct {
		OrganizationID, WorkspaceID string
		Basis                       Basis
		From, To                    time.Time
		ReportingCurrency           string
		Orders                      []OrderFact
		Settlements                 []SettlementFact
		Payments                    []PaymentFact
	}{in.OrganizationID, in.WorkspaceID, in.Basis, in.From, in.To, in.ReportingCurrency, orders, settlements, payments}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Calculate runs the deterministic normalize → deduplicate → aggregate →
// quality pipeline. It refuses mixed currencies when no reporting FX evidence
// is supplied; callers must run Task 089b conversion before passing facts.
func Calculate(in Input, generatedAt time.Time) (Snapshot, error) {
	if err := validateInput(in); err != nil {
		return Snapshot{}, err
	}
	if generatedAt.IsZero() || !generatedAt.Equal(generatedAt.UTC()) {
		return Snapshot{}, ErrInvalid
	}
	digest, err := digestInput(in)
	if err != nil {
		return Snapshot{}, fmt.Errorf("unit economics: input digest: %w", err)
	}
	settlementByRef := make(map[string]SettlementFact, len(in.Settlements))
	for _, fact := range in.Settlements {
		key := fact.SourceSystem + "\x00" + fact.SourceAccount + "\x00" + fact.EntryRef
		if existing, ok := settlementByRef[key]; ok {
			if existing.ID != fact.ID || existing.AmountMinor != fact.AmountMinor || existing.Currency != fact.Currency || existing.Kind != fact.Kind {
				return Snapshot{}, ErrConflict
			}
			continue
		}
		settlementByRef[key] = fact
	}
	rows := map[string]*rowAccumulator{}
	get := func(channel, currency string) *rowAccumulator {
		key := normalizeChannel(channel) + "\x00" + currency
		if rows[key] == nil {
			rows[key] = &rowAccumulator{row: Row{ChannelRef: normalizeChannel(channel), Currency: currency, Basis: in.Basis, Gross: component(0, currency, ValueObserved, "", nil), Discounts: component(0, currency, ValueObserved, "", nil), Cancellations: component(0, currency, ValueObserved, "", nil), Refunds: component(0, currency, ValueObserved, "", nil), NetRevenue: component(0, currency, ValueObserved, "", nil), Commission: component(0, currency, ValueObserved, "", nil), PaymentFee: component(0, currency, ValueObserved, "", nil), Logistics: component(0, currency, ValueMissing, "not_available", nil), Storage: component(0, currency, ValueMissing, "not_available", nil), Advertising: component(0, currency, ValueMissing, "not_available", nil), Promotion: component(0, currency, ValueMissing, "not_available", nil), COGS: component(0, currency, ValueMissing, "cost_snapshot_missing", nil), Penalties: component(0, currency, ValueObserved, "", nil), Compensation: component(0, currency, ValueObserved, "", nil), Payout: component(0, currency, ValueObserved, "", nil)}, refs: map[string]struct{}{}, reasons: map[string]struct{}{}}
		}
		return rows[key]
	}
	for _, order := range in.Orders {
		if order.OccurredAt.Before(in.From) || !order.OccurredAt.Before(in.To) {
			continue
		}
		if in.ReportingCurrency != "" && order.Currency != in.ReportingCurrency {
			return Snapshot{}, ErrMixedCurrency
		}
		a := get(order.ChannelRef, order.Currency)
		a.row.Orders++
		if err := addChecked(&a.row.UnitsMilli, order.QuantityMilli); err != nil {
			return Snapshot{}, err
		}
		for target, value := range map[*int64]int64{&a.row.Gross.MinorUnits: order.GrossMinor, &a.row.Discounts.MinorUnits: order.DiscountMinor} {
			if err := addChecked(target, value); err != nil {
				return Snapshot{}, err
			}
		}
		a.addRef("order:" + order.ID)
		if order.Status == "cancelled" {
			if err := addChecked(&a.row.Cancellations.MinorUnits, order.GrandMinor); err != nil {
				return Snapshot{}, err
			}
			a.reason("cancelled")
		}
		if order.RefundMinor > 0 {
			if err := addChecked(&a.row.Refunds.MinorUnits, order.RefundMinor); err != nil {
				return Snapshot{}, err
			}
			a.hasRefunds = true
		}
		if order.COGSMinor != nil {
			a.hasCOGS = true
			if err := addChecked(&a.row.COGS.MinorUnits, *order.COGSMinor); err != nil {
				return Snapshot{}, err
			}
			a.addRef("cogs:" + order.ID)
		}
	}
	settlementFeeByOrder := map[string]bool{}
	for _, fact := range settlementByRef {
		if fact.OccurredAt.Before(in.From) || !fact.OccurredAt.Before(in.To) {
			continue
		}
		if in.ReportingCurrency != "" && fact.Currency != in.ReportingCurrency {
			return Snapshot{}, ErrMixedCurrency
		}
		a := get(fact.ChannelRef, fact.Currency)
		a.addRef("settlement:" + fact.ID)
		value, err := absAmount(fact.AmountMinor)
		if err != nil {
			return Snapshot{}, err
		}
		switch fact.Kind {
		case "fee":
			if err := addChecked(&a.row.Commission.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.hasCommission = true
			if fact.OrderID != "" {
				settlementFeeByOrder[fact.OrderID] = true
			}
		case "refund":
			a.row.Refunds.MinorUnits += value
			a.hasRefunds = true
		case "payout":
			if err := addChecked(&a.row.Payout.MinorUnits, fact.AmountMinor); err != nil {
				return Snapshot{}, err
			}
			a.hasPayout = true
		case "sale": // order facts remain the revenue source; payout/sale is evidence only
		case "adjustment":
			a.reason("settlement_adjustment")
		case "penalty":
			if err := addChecked(&a.row.Penalties.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.hasPenalty = true
		case "compensation":
			if err := addChecked(&a.row.Compensation.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.hasCompensation = true
		case "logistics":
			if err := addChecked(&a.row.Logistics.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.row.Logistics.Status = ValueObserved
		case "storage":
			if err := addChecked(&a.row.Storage.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.row.Storage.Status = ValueObserved
		case "advertising":
			if err := addChecked(&a.row.Advertising.MinorUnits, value); err != nil {
				return Snapshot{}, err
			}
			a.row.Advertising.Status = ValueObserved
		default:
			a.row.QualityStatus = QualityUnsupported
			a.reason("unsupported_settlement_kind")
		}
		if fact.Disputed {
			a.hasDispute = true
			a.reason("disputed_settlement")
		}
	}
	for _, fact := range in.Payments {
		if fact.OccurredAt.Before(in.From) || !fact.OccurredAt.Before(in.To) {
			continue
		}
		if in.ReportingCurrency != "" && fact.Currency != in.ReportingCurrency {
			return Snapshot{}, ErrMixedCurrency
		}
		if settlementFeeByOrder[fact.OrderID] {
			continue
		}
		for _, a := range rows { // payment order attribution is resolved through the order source, never a provider name
			for _, order := range in.Orders {
				if order.ID == fact.OrderID && normalizeChannel(order.ChannelRef) == a.row.ChannelRef && order.Currency == fact.Currency {
					if err := addChecked(&a.row.PaymentFee.MinorUnits, fact.FeeMinor); err != nil {
						return Snapshot{}, err
					}
					a.hasPaymentFee = true
					a.addRef("payment:" + fact.ID)
					break
				}
			}
		}
	}
	resultRows := make([]Row, 0, len(rows))
	for _, a := range rows {
		if !a.hasCommission {
			a.row.Commission.Status = ValueMissing
			a.reason("commission_missing")
		}
		if !a.hasRefunds && a.row.Refunds.MinorUnits == 0 {
			a.row.Refunds.Status = ValueMissing
			a.reason("refund_fact_missing")
		}
		if !a.hasPayout {
			a.row.Payout.Status = ValueMissing
			a.reason("payout_missing")
		}
		if !a.hasPenalty {
			a.row.Penalties.Status = ValueMissing
		}
		if !a.hasCompensation {
			a.row.Compensation.Status = ValueMissing
		}
		if !a.hasPaymentFee {
			a.row.PaymentFee.Status = ValueMissing
			a.reason("payment_fee_missing")
		}
		a.row.NetRevenue.MinorUnits = a.row.Gross.MinorUnits - a.row.Discounts.MinorUnits - a.row.Cancellations.MinorUnits - a.row.Refunds.MinorUnits
		if a.row.NetRevenue.MinorUnits < 0 {
			a.conflict = true
			a.reason("net_revenue_negative")
		}
		profit := big.NewInt(a.row.NetRevenue.MinorUnits)
		for _, expense := range []int64{a.row.Commission.MinorUnits, a.row.PaymentFee.MinorUnits, a.row.Logistics.MinorUnits, a.row.Storage.MinorUnits, a.row.Advertising.MinorUnits, a.row.Promotion.MinorUnits, a.row.COGS.MinorUnits, a.row.Penalties.MinorUnits} {
			profit.Sub(profit, big.NewInt(expense))
		}
		profit.Add(profit, big.NewInt(a.row.Compensation.MinorUnits))
		if !profit.IsInt64() {
			return Snapshot{}, ErrInvalid
		}
		contribution := profit.Int64()
		a.row.Contribution.MinorUnits = contribution
		if a.row.NetRevenue.MinorUnits > 0 {
			numerator := new(big.Int).Mul(big.NewInt(contribution), big.NewInt(10000))
			denominator := big.NewInt(a.row.NetRevenue.MinorUnits)
			quotient := new(big.Int).Quo(numerator, denominator)
			if !quotient.IsInt64() {
				return Snapshot{}, ErrInvalid
			}
			a.row.MarginBasisPoints = quotient.Int64()
		}
		missing := 0
		for _, c := range []Component{a.row.Logistics, a.row.Storage, a.row.Advertising, a.row.Promotion, a.row.COGS, a.row.Commission, a.row.PaymentFee, a.row.Refunds, a.row.Payout} {
			if c.Status == ValueMissing {
				missing++
			}
		}
		a.row.CoveragePercent = int64((9 - missing) * 100 / 9)
		quality := QualityComplete
		if a.row.ChannelRef == UnattributedChannel {
			quality = QualityUnmatched
			a.reason("channel_unresolved")
		} else if missing > 0 {
			quality = QualityPartial
		}
		if a.hasDispute {
			quality = QualityConflict
		}
		if a.conflict {
			quality = QualityConflict
		}
		if a.row.QualityStatus == QualityUnsupported {
			quality = QualityUnsupported
		}
		a.row.QualityStatus = quality
		for _, c := range []*Component{&a.row.COGS, &a.row.Logistics, &a.row.Storage, &a.row.Advertising, &a.row.Promotion, &a.row.Contribution} {
			c.Currency = a.row.Currency
		}
		a.row.Contribution.Status = ValueEstimated
		if missing == 0 {
			a.row.Contribution.Status = ValueObserved
		}
		for key := range a.refs {
			a.row.SourceRefs = append(a.row.SourceRefs, key)
		}
		sort.Strings(a.row.SourceRefs)
		for key := range a.reasons {
			a.row.QualityReasons = append(a.row.QualityReasons, key)
		}
		sort.Strings(a.row.QualityReasons)
		a.row.InputDigest = digest
		resultRows = append(resultRows, a.row)
	}
	sort.Slice(resultRows, func(i, j int) bool {
		if resultRows[i].ChannelRef == resultRows[j].ChannelRef {
			return resultRows[i].Currency < resultRows[j].Currency
		}
		return resultRows[i].ChannelRef < resultRows[j].ChannelRef
	})
	quality := QualityComplete
	for _, row := range resultRows {
		if row.QualityStatus != QualityComplete {
			quality = row.QualityStatus
			break
		}
	}
	idDigest := sha256.Sum256([]byte(digest + string(in.Basis)))
	return Snapshot{ID: "ue-" + hex.EncodeToString(idDigest[:])[:24], GeneratedAt: generatedAt, OrganizationID: in.OrganizationID, WorkspaceID: in.WorkspaceID, From: in.From, To: in.To, Basis: in.Basis, ReportingCurrency: in.ReportingCurrency, AlgorithmVersion: AlgorithmVersion, MetricDefinitionVersion: MetricDefinitionVersion, AllocationPolicyVersion: AllocationPolicyVersion, ValuationPolicyVersion: ValuationPolicyVersion, AttributionPolicyVersion: AttributionPolicyVersion, InputDigest: digest, Rows: resultRows, QualityStatus: quality}, nil
}
