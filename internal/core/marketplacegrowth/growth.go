// Package marketplacegrowth contains the provider-neutral write-side model for
// promotions and advertising operations. It deliberately stops at a durable,
// approved intent until a connector has passed live qualification.
package marketplacegrowth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"
)

const MaxPreviewRows = 1000

var (
	ErrInvalid          = errors.New("marketplace growth: invalid value")
	ErrFloorViolation   = errors.New("marketplace growth: floor or margin guard violation")
	ErrApprovalRequired = errors.New("marketplace growth: approval required")
	ErrConflict         = errors.New("marketplace growth: conflicting retry")
)

// Operation names are provider-neutral. Connectors map them to their own
// remote vocabulary only after a capability-level qualification.
type OperationName string

const (
	OperationPromotionApply  OperationName = "promotion.apply"
	OperationCampaignCreate  OperationName = "campaign.create"
	OperationCampaignLaunch  OperationName = "campaign.launch"
	OperationCampaignPause   OperationName = "campaign.pause"
	OperationCampaignResume  OperationName = "campaign.resume"
	OperationCampaignStop    OperationName = "campaign.stop"
	OperationCampaignArchive OperationName = "campaign.archive"
	OperationCampaignLink    OperationName = "campaign.link_products"
	OperationBidUpdate       OperationName = "bid.update"
	OperationBudgetUpdate    OperationName = "budget.update"
	OperationKillSwitch      OperationName = "kill_switch.enable"
)

func (o OperationName) Valid() bool {
	switch o {
	case OperationPromotionApply, OperationCampaignCreate, OperationCampaignLaunch, OperationCampaignPause,
		OperationCampaignResume, OperationCampaignStop, OperationCampaignArchive, OperationCampaignLink,
		OperationBidUpdate, OperationBudgetUpdate, OperationKillSwitch:
		return true
	default:
		return false
	}
}

func (o OperationName) Capability() string {
	if strings.HasPrefix(string(o), "promotion.") {
		return "promotions.manage"
	}
	return "ads.manage"
}

// BidUnit prevents an amount from being interpreted differently by clients.
type BidUnit string

const (
	BidCPC BidUnit = "cpc"
	BidCPM BidUnit = "cpm"
	BidCPA BidUnit = "cpa"
)

func (u BidUnit) Valid() bool { return u == BidCPC || u == BidCPM || u == BidCPA }

// Candidate is the bounded, already-normalized input for one SKU. Missing or
// stale financial facts are explicit and never treated as zero.
type Candidate struct {
	SKU                string `json:"sku"`
	Currency           string `json:"currency"`
	CurrentPriceMinor  int64  `json:"current_price_minor"`
	ProposedPriceMinor int64  `json:"proposed_price_minor"`
	UnitCostMinor      int64  `json:"unit_cost_minor"`
	CommissionBPS      int64  `json:"commission_basis_points"`
	LogisticsMinor     int64  `json:"logistics_minor"`
	AdvertisingMinor   int64  `json:"advertising_minor"`
	DiscountBPS        int64  `json:"discount_basis_points"`
	SubsidyMinor       int64  `json:"subsidy_minor"`
	Stock              int64  `json:"stock"`
	FactsFresh         bool   `json:"facts_fresh"`
	Eligible           bool   `json:"eligible"`
	Conflict           bool   `json:"conflict"`
}

// PreviewRequest is the shared input for promotion participation, campaign
// lifecycle, bid and budget changes. It contains no credentials or raw remote
// payloads.
type PreviewRequest struct {
	Operation           OperationName `json:"operation"`
	ChannelID           string        `json:"channel_id"`
	AccountID           string        `json:"account_id"`
	TargetID            string        `json:"target_id"`
	Currency            string        `json:"currency"`
	FloorPriceMinor     int64         `json:"floor_price_minor"`
	MinimumMarginBPS    int64         `json:"minimum_margin_basis_points"`
	ApprovalThreshold   int           `json:"approval_threshold"`
	ProposedBidMinor    int64         `json:"proposed_bid_minor"`
	MaximumBidMinor     int64         `json:"maximum_bid_minor"`
	ProposedBudgetMinor int64         `json:"proposed_budget_minor"`
	MaximumBudgetMinor  int64         `json:"maximum_budget_minor"`
	BidUnit             BidUnit       `json:"bid_unit,omitempty"`
	Strategy            string        `json:"strategy,omitempty"`
	Items               []Candidate   `json:"items"`
}

// PreviewRow is the operator-facing before/after result for one SKU.
type PreviewRow struct {
	SKU                 string   `json:"sku"`
	Decision            string   `json:"decision"`
	ReasonCodes         []string `json:"reason_codes,omitempty"`
	CurrentPriceMinor   int64    `json:"current_price_minor"`
	EffectivePriceMinor int64    `json:"effective_price_minor"`
	DiscountMinor       int64    `json:"discount_minor"`
	SubsidyMinor        int64    `json:"subsidy_minor"`
	CommissionMinor     int64    `json:"commission_minor"`
	LogisticsMinor      int64    `json:"logistics_minor"`
	AdvertisingMinor    int64    `json:"advertising_minor"`
	UnitCostMinor       int64    `json:"unit_cost_minor"`
	ContributionMinor   int64    `json:"contribution_minor"`
	MarginBPS           int64    `json:"margin_basis_points"`
	FloorPriceMinor     int64    `json:"floor_price_minor"`
	Stock               int64    `json:"stock"`
	StockRisk           bool     `json:"stock_risk"`
}

const (
	DecisionApplied         = "applied"
	DecisionRejected        = "rejected"
	DecisionUnknown         = "unknown"
	DecisionManualAttention = "manual_attention"
)

// Preview is immutable qualification evidence. A later apply must reference
// this exact digest and current version; it cannot silently recompute a plan.
type Preview struct {
	ID               string        `json:"id"`
	Operation        OperationName `json:"operation"`
	ChannelID        string        `json:"channel_id"`
	AccountID        string        `json:"account_id"`
	TargetID         string        `json:"target_id"`
	Currency         string        `json:"currency"`
	InputDigest      string        `json:"input_digest"`
	RuleVersion      int64         `json:"rule_version"`
	Rows             []PreviewRow  `json:"rows"`
	AffectedCount    int           `json:"affected_count"`
	EligibleCount    int           `json:"eligible_count"`
	BlockedCount     int           `json:"blocked_count"`
	ApprovalRequired bool          `json:"approval_required"`
	State            string        `json:"state"`
	CreatedAt        time.Time     `json:"created_at"`
}

const (
	PreviewReady            = "ready"
	PreviewApprovalRequired = "approval_required"
	PreviewBlocked          = "blocked"
)

// Operation is the durable intent created after a matching approval. It is
// explicitly qualification_required while no credentialed connector writer is
// admitted, so the UI cannot present a queued intent as a remote success.
type Operation struct {
	ID                   string        `json:"id"`
	PreviewID            string        `json:"preview_id"`
	IdempotencyKey       string        `json:"idempotency_key"`
	ApprovalRequestID    string        `json:"approval_request_id"`
	Operation            OperationName `json:"operation"`
	Capability           string        `json:"capability"`
	ChannelID            string        `json:"channel_id"`
	AccountID            string        `json:"account_id"`
	TargetID             string        `json:"target_id"`
	InputDigest          string        `json:"input_digest"`
	State                string        `json:"state"`
	RemoteWriteQualified bool          `json:"remote_write_qualified"`
	RemoteOperationID    string        `json:"remote_operation_id,omitempty"`
	ErrorCode            string        `json:"error_code,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

const (
	StateAccepted              = "accepted"
	StateApplied               = "applied"
	StateRejected              = "rejected"
	StateConflict              = "conflict"
	StateRateLimited           = "rate_limited"
	StateUnknown               = "unknown"
	StateManualAttention       = "manual_attention"
	StateQualificationRequired = "qualification_required"
)

// Drift is append-only reconciliation evidence.
type Drift struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	Kind        string    `json:"kind"`
	Expected    string    `json:"expected"`
	Actual      string    `json:"actual"`
	Severity    string    `json:"severity"`
	ObservedAt  time.Time `json:"observed_at"`
}

// Observation is the sanitized read-after-write result from a connector.
type Observation struct {
	OperationID string    `json:"operation_id"`
	State       string    `json:"state"`
	InputDigest string    `json:"input_digest"`
	AppliedRows int       `json:"applied_rows"`
	ObservedAt  time.Time `json:"observed_at"`
}

// KillSwitch is tenant-scoped control state for all growth writes.
type KillSwitch struct {
	Enabled   bool      `json:"enabled"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Rule is the compact read model shown in the promotions tab.
type Rule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ChannelID        string    `json:"channel_id"`
	Kind             string    `json:"kind"`
	Status           string    `json:"status"`
	Currency         string    `json:"currency"`
	DiscountBPS      int64     `json:"discount_basis_points"`
	SubsidyMinor     int64     `json:"subsidy_minor"`
	FloorPriceMinor  int64     `json:"floor_price_minor"`
	MinimumMarginBPS int64     `json:"minimum_margin_basis_points"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	Version          int64     `json:"version"`
}

func (r PreviewRequest) Validate() error {
	if !r.Operation.Valid() || !validRef(r.ChannelID) || !validRef(r.AccountID) || !validRef(r.TargetID) || !validCurrency(r.Currency) || r.FloorPriceMinor < 0 || r.MinimumMarginBPS < 0 || r.MinimumMarginBPS > 10000 || r.ApprovalThreshold < 0 || r.ApprovalThreshold > MaxPreviewRows || r.ProposedBidMinor < 0 || r.MaximumBidMinor < 0 || r.ProposedBudgetMinor < 0 || r.MaximumBudgetMinor < 0 || len(r.Items) == 0 || len(r.Items) > MaxPreviewRows {
		return ErrInvalid
	}
	if r.FloorPriceMinor == 0 {
		return ErrInvalid
	}
	if r.Operation == OperationBidUpdate && (!r.BidUnit.Valid() || r.ProposedBidMinor <= 0 || r.MaximumBidMinor <= 0 || r.ProposedBidMinor > r.MaximumBidMinor) {
		return ErrInvalid
	}
	if r.Operation == OperationBudgetUpdate && (r.ProposedBudgetMinor <= 0 || r.MaximumBudgetMinor <= 0 || r.ProposedBudgetMinor > r.MaximumBudgetMinor) {
		return ErrInvalid
	}
	if len(r.Strategy) > 128 || (r.Strategy != "" && strings.TrimSpace(r.Strategy) != r.Strategy) {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(r.Items))
	for _, item := range r.Items {
		if !validRef(item.SKU) || item.Currency != r.Currency || item.CurrentPriceMinor < 0 || item.ProposedPriceMinor < 0 || item.UnitCostMinor < 0 || item.CommissionBPS < 0 || item.CommissionBPS > 10000 || item.LogisticsMinor < 0 || item.AdvertisingMinor < 0 || item.DiscountBPS < 0 || item.DiscountBPS > 10000 || item.SubsidyMinor < 0 || item.Stock < 0 {
			return ErrInvalid
		}
		if _, exists := seen[item.SKU]; exists {
			return ErrInvalid
		}
		seen[item.SKU] = struct{}{}
	}
	return nil
}

// BuildPreview computes effective price, proceeds, contribution and margin
// using integer minor units and basis points. It also marks stale facts,
// conflicts, floor violations and stock risk per row.
func BuildPreview(id string, request PreviewRequest, ruleVersion int64, now time.Time) (Preview, error) {
	if !validRef(id) || ruleVersion < 1 || now.IsZero() || now.Location() != time.UTC || request.Validate() != nil {
		return Preview{}, ErrInvalid
	}
	items := append([]Candidate(nil), request.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKU < items[j].SKU })
	out := Preview{ID: id, Operation: request.Operation, ChannelID: request.ChannelID, AccountID: request.AccountID, TargetID: request.TargetID, Currency: request.Currency, RuleVersion: ruleVersion, AffectedCount: len(items), Rows: make([]PreviewRow, 0, len(items)), CreatedAt: now}
	for _, item := range items {
		row, err := calculateRow(request, item)
		if err != nil {
			return Preview{}, err
		}
		out.Rows = append(out.Rows, row)
		if row.Decision == DecisionApplied {
			out.EligibleCount++
		} else {
			out.BlockedCount++
		}
	}
	out.InputDigest = digest(request)
	out.ApprovalRequired = isMutating(request.Operation) && out.EligibleCount > 0
	switch {
	case out.EligibleCount == 0:
		out.State = PreviewBlocked
	case out.ApprovalRequired:
		out.State = PreviewApprovalRequired
	default:
		out.State = PreviewReady
	}
	return out, nil
}

func calculateRow(request PreviewRequest, item Candidate) (PreviewRow, error) {
	price := item.ProposedPriceMinor
	if price == 0 {
		price = item.CurrentPriceMinor
	}
	discount, err := ratioAmount(price, item.DiscountBPS)
	if err != nil {
		return PreviewRow{}, err
	}
	effective, err := subtract(price, discount)
	if err != nil {
		return PreviewRow{}, err
	}
	commission, err := ratioAmount(effective, item.CommissionBPS)
	if err != nil {
		return PreviewRow{}, err
	}
	contribution, err := subtract(effective, item.UnitCostMinor, item.LogisticsMinor, item.AdvertisingMinor, commission)
	if err != nil {
		return PreviewRow{}, err
	}
	contribution, err = add(contribution, item.SubsidyMinor)
	if err != nil {
		return PreviewRow{}, err
	}
	margin := ratio(effective, contribution)
	row := PreviewRow{SKU: item.SKU, Decision: DecisionApplied, CurrentPriceMinor: item.CurrentPriceMinor, EffectivePriceMinor: effective, DiscountMinor: discount, SubsidyMinor: item.SubsidyMinor, CommissionMinor: commission, LogisticsMinor: item.LogisticsMinor, AdvertisingMinor: item.AdvertisingMinor, UnitCostMinor: item.UnitCostMinor, ContributionMinor: contribution, MarginBPS: margin, FloorPriceMinor: request.FloorPriceMinor, Stock: item.Stock, StockRisk: item.Stock == 0}
	if !item.FactsFresh {
		row.Decision = DecisionManualAttention
		row.ReasonCodes = append(row.ReasonCodes, "stale_or_missing_facts")
	}
	if !item.Eligible {
		row.Decision = DecisionRejected
		row.ReasonCodes = append(row.ReasonCodes, "not_eligible")
	}
	if item.Conflict {
		row.Decision = DecisionUnknown
		row.ReasonCodes = append(row.ReasonCodes, "remote_conflict")
	}
	if effective < request.FloorPriceMinor {
		row.Decision = DecisionRejected
		row.ReasonCodes = append(row.ReasonCodes, "floor_price")
	}
	if effective == 0 || margin < request.MinimumMarginBPS {
		row.Decision = DecisionRejected
		row.ReasonCodes = append(row.ReasonCodes, "minimum_margin")
	}
	if row.StockRisk {
		row.ReasonCodes = append(row.ReasonCodes, "stock_risk")
	}
	if request.Operation == OperationBidUpdate && request.ProposedBidMinor > request.MaximumBidMinor {
		row.Decision = DecisionRejected
		row.ReasonCodes = append(row.ReasonCodes, "maximum_bid")
	}
	if request.Operation == OperationBudgetUpdate && request.ProposedBudgetMinor > request.MaximumBudgetMinor {
		row.Decision = DecisionRejected
		row.ReasonCodes = append(row.ReasonCodes, "maximum_budget")
	}
	sort.Strings(row.ReasonCodes)
	return row, nil
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (p Preview) Validate() error {
	if !validRef(p.ID) || !p.Operation.Valid() || !validRef(p.ChannelID) || !validRef(p.AccountID) || !validRef(p.TargetID) || !validCurrency(p.Currency) || len(p.InputDigest) != 64 || p.InputDigest != strings.ToLower(p.InputDigest) || p.RuleVersion < 1 || len(p.Rows) != p.AffectedCount || p.AffectedCount < 1 || p.AffectedCount > MaxPreviewRows || p.EligibleCount < 0 || p.BlockedCount != p.AffectedCount-p.EligibleCount || p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC || (p.State != PreviewReady && p.State != PreviewApprovalRequired && p.State != PreviewBlocked) {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, row := range p.Rows {
		if !validRef(row.SKU) || (row.Decision != DecisionApplied && row.Decision != DecisionRejected && row.Decision != DecisionUnknown && row.Decision != DecisionManualAttention) || row.EffectivePriceMinor < 0 || row.CurrentPriceMinor < 0 || row.DiscountMinor < 0 || row.SubsidyMinor < 0 || row.CommissionMinor < 0 || row.LogisticsMinor < 0 || row.AdvertisingMinor < 0 || row.UnitCostMinor < 0 || row.Stock < 0 || row.MarginBPS < -100000 || row.MarginBPS > 10000 {
			return ErrInvalid
		}
		if _, ok := seen[row.SKU]; ok {
			return ErrInvalid
		}
		seen[row.SKU] = struct{}{}
	}
	return nil
}

func NewOperation(id, idempotencyKey, approvalID string, preview Preview, now time.Time) (Operation, error) {
	if preview.Validate() != nil || !validRef(id) || !validRef(idempotencyKey) || !validRef(approvalID) || now.IsZero() || now.Location() != time.UTC || !preview.ApprovalRequired || preview.EligibleCount == 0 {
		return Operation{}, ErrInvalid
	}
	return Operation{ID: id, PreviewID: preview.ID, IdempotencyKey: idempotencyKey, ApprovalRequestID: approvalID, Operation: preview.Operation, Capability: preview.Operation.Capability(), ChannelID: preview.ChannelID, AccountID: preview.AccountID, TargetID: preview.TargetID, InputDigest: preview.InputDigest, State: StateQualificationRequired, CreatedAt: now, UpdatedAt: now}, nil
}

func (o Operation) Validate() error {
	if !validRef(o.ID) || !validRef(o.PreviewID) || !validRef(o.IdempotencyKey) || !validRef(o.ApprovalRequestID) || !o.Operation.Valid() || o.Capability != o.Operation.Capability() || !validRef(o.ChannelID) || !validRef(o.AccountID) || !validRef(o.TargetID) || len(o.InputDigest) != 64 || o.State == "" || !validState(o.State) || o.CreatedAt.IsZero() || o.UpdatedAt.IsZero() || o.CreatedAt.Location() != time.UTC || o.UpdatedAt.Location() != time.UTC || o.UpdatedAt.Before(o.CreatedAt) {
		return ErrInvalid
	}
	return nil
}

func Reconcile(expected Operation, observation Observation) ([]Drift, error) {
	if expected.Validate() != nil || !validRef(observation.OperationID) || observation.OperationID != expected.ID || !validState(observation.State) || len(observation.InputDigest) != 64 || observation.AppliedRows < 0 || observation.AppliedRows > MaxPreviewRows || observation.ObservedAt.IsZero() || observation.ObservedAt.Location() != time.UTC {
		return nil, ErrInvalid
	}
	var result []Drift
	if expected.InputDigest != observation.InputDigest {
		result = append(result, Drift{ID: digest(expected.ID + ":digest"), OperationID: expected.ID, Kind: "input_digest_mismatch", Expected: expected.InputDigest, Actual: observation.InputDigest, Severity: "critical", ObservedAt: observation.ObservedAt})
	}
	if expected.State != observation.State {
		result = append(result, Drift{ID: digest(expected.ID + ":state:" + observation.State), OperationID: expected.ID, Kind: "state_mismatch", Expected: expected.State, Actual: observation.State, Severity: "high", ObservedAt: observation.ObservedAt})
	}
	if observation.State == StateApplied && observation.AppliedRows == 0 {
		result = append(result, Drift{ID: digest(expected.ID + ":empty"), OperationID: expected.ID, Kind: "applied_without_rows", Expected: ">0", Actual: "0", Severity: "high", ObservedAt: observation.ObservedAt})
	}
	return result, nil
}

func (k KillSwitch) Validate() error {
	return func() error {
		if len(k.Reason) > 500 || strings.TrimSpace(k.Reason) != k.Reason || k.UpdatedAt.IsZero() || k.UpdatedAt.Location() != time.UTC {
			return ErrInvalid
		}
		return nil
	}()
}

func DemoRules(now time.Time) []Rule {
	return []Rule{{ID: "demo-promo-1", Name: "Сезонная скидка", ChannelID: "demo", Kind: "discount", Status: "preview", Currency: "RUB", DiscountBPS: 1000, FloorPriceMinor: 99000, MinimumMarginBPS: 1500, StartsAt: now, EndsAt: now.Add(14 * 24 * time.Hour), Version: 1}}
}

// Validate checks a promotion rule before it is stored or shown as demo data.
func (r Rule) Validate() error {
	if !validRef(r.ID) || !validRef(r.Name) || !validRef(r.ChannelID) || (r.Kind != "discount" && r.Kind != "coupon" && r.Kind != "subsidy") || (r.Status != "draft" && r.Status != "preview" && r.Status != "active" && r.Status != "ended" && r.Status != "unknown") || !validCurrency(r.Currency) || r.DiscountBPS < 0 || r.DiscountBPS > 10000 || r.SubsidyMinor < 0 || r.FloorPriceMinor <= 0 || r.MinimumMarginBPS < 0 || r.MinimumMarginBPS > 10000 || r.Version < 1 || r.StartsAt.IsZero() || r.EndsAt.IsZero() || r.StartsAt.Location() != time.UTC || r.EndsAt.Location() != time.UTC || !r.EndsAt.After(r.StartsAt) {
		return ErrInvalid
	}
	return nil
}

func isMutating(o OperationName) bool { return o.Valid() }

func validState(value string) bool {
	switch value {
	case StateAccepted, StateApplied, StateRejected, StateConflict, StateRateLimited, StateUnknown, StateManualAttention, StateQualificationRequired:
		return true
	default:
		return false
	}
}

func add(values ...int64) (int64, error) {
	total := big.NewInt(0)
	for _, value := range values {
		total.Add(total, big.NewInt(value))
	}
	if !total.IsInt64() {
		return 0, ErrInvalid
	}
	return total.Int64(), nil
}

func subtract(first int64, rest ...int64) (int64, error) {
	total := big.NewInt(first)
	for _, value := range rest {
		total.Sub(total, big.NewInt(value))
	}
	if !total.IsInt64() {
		return 0, ErrInvalid
	}
	return total.Int64(), nil
}

func ratioAmount(amount, bps int64) (int64, error) {
	if amount < 0 || bps < 0 || bps > 10000 {
		return 0, ErrInvalid
	}
	result := new(big.Int).Mul(big.NewInt(amount), big.NewInt(bps))
	result.Quo(result, big.NewInt(10000))
	if !result.IsInt64() {
		return 0, ErrInvalid
	}
	return result.Int64(), nil
}

func ratio(denominator, numerator int64) int64 {
	if denominator <= 0 {
		if numerator < 0 {
			return -10000
		}
		return 0
	}
	result := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(10000))
	result.Quo(result, big.NewInt(denominator))
	if !result.IsInt64() {
		return 0
	}
	return result.Int64()
}

func validRef(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n/")
}

func validCurrency(value string) bool {
	return len(value) == 3 && value == strings.ToUpper(value) && value >= "A" && value <= "ZZZ"
}
