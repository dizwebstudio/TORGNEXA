package replenishment

// This file contains the provider-neutral planning contracts introduced by
// Task 165. Forecasts and recommendations are derived facts: they never mutate
// the inventory ledger or submit a purchase order by themselves.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalidPlanning    = errors.New("replenishment: invalid planning value")
	ErrInsufficientData   = errors.New("replenishment: insufficient demand data")
	ErrUnitMismatch       = errors.New("replenishment: quantity unit mismatch")
	ErrPlanningConflict   = errors.New("replenishment: planning conflict")
	ErrModeNotAllowed     = errors.New("replenishment: operating mode is not allowed")
	ErrRecommendationHold = errors.New("replenishment: recommendation is on hold")
)

var planningReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var planningReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func validPlanningText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 192 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// OperatingMode controls how a recommendation may be materialized. AutoSubmit
// is deliberately opt-in and still requires approval/capability checks in the
// procurement runtime.
type OperatingMode string

const (
	RecommendationOnly OperatingMode = "recommendation_only"
	DraftPO            OperatingMode = "draft_po"
	AutoSubmit         OperatingMode = "auto_submit"
)

func (m OperatingMode) Valid() bool {
	return m == RecommendationOnly || m == DraftPO || m == AutoSubmit
}

// PlanningStatus describes data quality rather than operational stock state.
type PlanningStatus string

const (
	PlanningHealthy     PlanningStatus = "healthy"
	PlanningDegraded    PlanningStatus = "degraded"
	PlanningUnavailable PlanningStatus = "unavailable"
)

func (s PlanningStatus) Valid() bool {
	return s == PlanningHealthy || s == PlanningDegraded || s == PlanningUnavailable
}

// PlanningQuality is bounded evidence about the inputs used by a derived
// forecast. Warnings are machine reason codes, never raw provider messages.
type PlanningQuality struct {
	Status           PlanningStatus
	FreshnessSeconds int64
	CoverageBPS      int64
	SampleCount      int64
	Warnings         []string
}

func (q PlanningQuality) Validate() error {
	if !q.Status.Valid() || q.FreshnessSeconds < 0 || q.CoverageBPS < 0 || q.CoverageBPS > 10_000 || q.SampleCount < 0 || len(q.Warnings) > 32 {
		return ErrInvalidPlanning
	}
	for _, warning := range q.Warnings {
		if !planningReasonPattern.MatchString(warning) {
			return ErrInvalidPlanning
		}
	}
	return nil
}

// PlanningGrain identifies one independent forecast series.
type PlanningGrain struct {
	OfferID      string
	SKU          string
	WarehouseID  string
	SalesChannel string
}

func (g PlanningGrain) Validate() error {
	if !planningReferencePattern.MatchString(g.OfferID) || !planningReferencePattern.MatchString(g.SKU) || !planningReferencePattern.MatchString(g.WarehouseID) {
		return ErrInvalidPlanning
	}
	if g.SalesChannel != "" && !planningReferencePattern.MatchString(g.SalesChannel) {
		return ErrInvalidPlanning
	}
	return nil
}

func (g PlanningGrain) key() string {
	return strings.Join([]string{g.OfferID, g.SKU, g.WarehouseID, g.SalesChannel}, "\x00")
}

// DemandObservation is a normalized demand fact. Returns/cancellations are
// netted explicitly; they are never silently treated as zero.
type DemandObservation struct {
	ID          string
	Grain       PlanningGrain
	BucketStart time.Time
	ObservedAt  time.Time
	Quantity    domain.Quantity
	Returns     domain.Quantity
	Source      string
}

func (o DemandObservation) Validate() error {
	if !planningReferencePattern.MatchString(o.ID) || o.Grain.Validate() != nil || !utcPlanning(o.BucketStart) || !utcPlanning(o.ObservedAt) || o.Quantity.Validate() != nil || o.Returns.Validate() != nil || !planningReferencePattern.MatchString(o.Source) {
		return ErrInvalidPlanning
	}
	if o.Quantity.Unit != o.Returns.Unit {
		return ErrUnitMismatch
	}
	if cmp, err := o.Returns.Value.Cmp(o.Quantity.Value); err != nil || cmp > 0 {
		return ErrInvalidPlanning
	}
	if cmp, err := o.Quantity.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	return nil
}

// NetDemand returns fulfilled demand after verified returns/cancellations.
func (o DemandObservation) NetDemand() (domain.Quantity, error) {
	if err := o.Validate(); err != nil {
		return domain.Quantity{}, err
	}
	value, err := o.Quantity.Value.Sub(o.Returns.Value)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(value, o.Quantity.Unit)
}

// ForecastRun is the immutable execution metadata for one planning pass.
type ForecastRun struct {
	ID               string
	OrganizationID   string
	WorkspaceID      string
	AlgorithmVersion string
	InputDigest      string
	HorizonDays      int
	GeneratedAt      time.Time
	ValidUntil       time.Time
	Status           string
	Quality          PlanningQuality
	Version          int64
}

func (r ForecastRun) Validate() error {
	if !planningReferencePattern.MatchString(r.ID) || !planningReferencePattern.MatchString(r.OrganizationID) || !planningReferencePattern.MatchString(r.WorkspaceID) || !planningReferencePattern.MatchString(r.AlgorithmVersion) || !hexDigestPlanning(r.InputDigest) || r.HorizonDays < 1 || r.HorizonDays > 366 || !utcPlanning(r.GeneratedAt) || !utcPlanning(r.ValidUntil) || r.ValidUntil.Before(r.GeneratedAt) || (r.Status != "running" && r.Status != "completed" && r.Status != "failed") || r.Quality.Validate() != nil || r.Version < 1 {
		return ErrInvalidPlanning
	}
	return nil
}

// NewForecastRun binds a run to a deterministic normalized input digest.
func NewForecastRun(id, organizationID, workspaceID, algorithmVersion string, horizonDays int, observations []DemandObservation, generatedAt time.Time) (ForecastRun, error) {
	digest, err := DigestObservations(observations)
	if err != nil {
		return ForecastRun{}, err
	}
	if horizonDays < 1 || horizonDays > 366 || !utcPlanning(generatedAt) || !planningReferencePattern.MatchString(id) || !planningReferencePattern.MatchString(organizationID) || !planningReferencePattern.MatchString(workspaceID) || !planningReferencePattern.MatchString(algorithmVersion) {
		return ForecastRun{}, ErrInvalidPlanning
	}
	return ForecastRun{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID, AlgorithmVersion: algorithmVersion, InputDigest: digest, HorizonDays: horizonDays, GeneratedAt: generatedAt, ValidUntil: generatedAt.Add(24 * time.Hour), Status: "running", Quality: PlanningQuality{Status: PlanningHealthy, CoverageBPS: 10_000, SampleCount: int64(len(observations))}, Version: 1}, nil
}

// ForecastPoint is a bounded point/upper forecast for one grain and period.
type ForecastPoint struct {
	RunID         string
	InputDigest   string
	Grain         PlanningGrain
	PeriodStart   time.Time
	PeriodDays    int
	DemandP50     domain.Quantity
	DemandP90     domain.Quantity
	ConfidenceBPS int64
	SampleCount   int64
	Explanation   string
	GeneratedAt   time.Time
	ValidUntil    time.Time
}

func (p ForecastPoint) Validate() error {
	if !planningReferencePattern.MatchString(p.RunID) || !hexDigestPlanning(p.InputDigest) || p.Grain.Validate() != nil || !utcPlanning(p.PeriodStart) || p.PeriodDays < 1 || p.PeriodDays > 366 || p.DemandP50.Validate() != nil || p.DemandP90.Validate() != nil || p.DemandP50.Unit != p.DemandP90.Unit || p.ConfidenceBPS < 0 || p.ConfidenceBPS > 10_000 || p.SampleCount < 0 || !validPlanningText(p.Explanation) || !utcPlanning(p.GeneratedAt) || !utcPlanning(p.ValidUntil) || p.ValidUntil.Before(p.GeneratedAt) {
		return ErrInvalidPlanning
	}
	if cmp, err := p.DemandP90.Value.Cmp(p.DemandP50.Value); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	return nil
}

// StockProjection contains only derived stock planning facts. Shortfall keeps
// an at-risk quantity visible instead of hiding it by clamping projected stock.
type StockProjection struct {
	RunID              string
	Grain              PlanningGrain
	PeriodStart        time.Time
	OpeningAvailable   domain.Quantity
	ConfirmedInbound   domain.Quantity
	ForecastDemand     domain.Quantity
	ProjectedAvailable domain.Quantity
	Shortfall          domain.Quantity
	DaysOfSupply       domain.Decimal
	StockoutRisk       bool
	OverstockRisk      bool
	Explanation        string
}

func (p StockProjection) Validate() error {
	if !planningReferencePattern.MatchString(p.RunID) || p.Grain.Validate() != nil || !utcPlanning(p.PeriodStart) || p.OpeningAvailable.Validate() != nil || p.ConfirmedInbound.Validate() != nil || p.ForecastDemand.Validate() != nil || p.ProjectedAvailable.Validate() != nil || p.Shortfall.Validate() != nil || p.DaysOfSupply.Validate() != nil || !validPlanningText(p.Explanation) {
		return ErrInvalidPlanning
	}
	if p.OpeningAvailable.Unit != p.ConfirmedInbound.Unit || p.OpeningAvailable.Unit != p.ForecastDemand.Unit || p.OpeningAvailable.Unit != p.ProjectedAvailable.Unit || p.OpeningAvailable.Unit != p.Shortfall.Unit {
		return ErrUnitMismatch
	}
	if cmp, err := p.OpeningAvailable.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	if cmp, err := p.ConfirmedInbound.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	if cmp, err := p.ForecastDemand.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	if cmp, err := p.ProjectedAvailable.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	if cmp, err := p.Shortfall.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
		return ErrInvalidPlanning
	}
	return nil
}

// ProjectStock computes opening + confirmed inbound - demand with an explicit
// non-negative projected balance and shortfall evidence.
func ProjectStock(runID string, grain PlanningGrain, periodStart time.Time, opening, inbound, demand domain.Quantity) (StockProjection, error) {
	if !planningReferencePattern.MatchString(runID) || grain.Validate() != nil || !utcPlanning(periodStart) || opening.Validate() != nil || inbound.Validate() != nil || demand.Validate() != nil {
		return StockProjection{}, ErrInvalidPlanning
	}
	if opening.Unit != inbound.Unit || opening.Unit != demand.Unit {
		return StockProjection{}, ErrUnitMismatch
	}
	for _, value := range []domain.Quantity{opening, inbound, demand} {
		if cmp, err := value.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
			return StockProjection{}, ErrInvalidPlanning
		}
	}
	available, err := opening.Value.Add(inbound.Value)
	if err != nil {
		return StockProjection{}, err
	}
	available, err = available.Sub(demand.Value)
	if err != nil {
		return StockProjection{}, err
	}
	projected := available
	shortfall, err := domain.NewDecimal(0, 0)
	if cmp, cmpErr := available.Cmp(zeroDecimal()); cmpErr != nil {
		return StockProjection{}, cmpErr
	} else if cmp < 0 {
		shortfall, err = domain.NewDecimal(-available.Coefficient(), available.Scale())
		if err != nil {
			return StockProjection{}, err
		}
		projected = zeroDecimal()
	}
	projectedQuantity, err := domain.NewQuantity(projected, opening.Unit)
	if err != nil {
		return StockProjection{}, err
	}
	shortfallQuantity, err := domain.NewQuantity(shortfall, opening.Unit)
	if err != nil {
		return StockProjection{}, err
	}
	return StockProjection{RunID: runID, Grain: grain, PeriodStart: periodStart, OpeningAvailable: opening, ConfirmedInbound: inbound, ForecastDemand: demand, ProjectedAvailable: projectedQuantity, Shortfall: shortfallQuantity, StockoutRisk: !shortfall.IsZero(), Explanation: fmt.Sprintf("opening=%s + inbound=%s - demand=%s", opening.Value.String(), inbound.Value.String(), demand.Value.String())}, nil
}

// ReorderPolicy is the bounded workspace policy used to turn a projection into
// an explainable recommendation.
type ReorderPolicy struct {
	ID              string
	OrganizationID  string
	WorkspaceID     string
	Grain           PlanningGrain
	SupplierOfferID string
	Mode            OperatingMode
	TargetDays      int
	ReviewDays      int
	SafetyStock     domain.Quantity
	MOQ             domain.Quantity
	CasePack        domain.Quantity
	MaxOrder        domain.Quantity
	Budget          domain.Money
	ServiceLevelBPS int64
	Enabled         bool
	Version         int64
	UpdatedAt       time.Time
}

func (p ReorderPolicy) Validate() error {
	if !planningReferencePattern.MatchString(p.ID) || !planningReferencePattern.MatchString(p.OrganizationID) || !planningReferencePattern.MatchString(p.WorkspaceID) || p.Grain.Validate() != nil || !planningReferencePattern.MatchString(p.SupplierOfferID) || !p.Mode.Valid() || p.TargetDays < 1 || p.TargetDays > 366 || p.ReviewDays < 1 || p.ReviewDays > 90 || p.SafetyStock.Validate() != nil || p.MOQ.Validate() != nil || p.CasePack.Validate() != nil || p.MaxOrder.Validate() != nil || p.Budget.Validate() != nil || p.ServiceLevelBPS < 0 || p.ServiceLevelBPS > 10_000 || p.Version < 1 || !utcPlanning(p.UpdatedAt) {
		return ErrInvalidPlanning
	}
	if p.SafetyStock.Unit != p.MOQ.Unit || p.SafetyStock.Unit != p.CasePack.Unit || p.SafetyStock.Unit != p.MaxOrder.Unit {
		return ErrUnitMismatch
	}
	for _, value := range []domain.Quantity{p.SafetyStock, p.MOQ, p.CasePack, p.MaxOrder} {
		if cmp, err := value.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
			return ErrInvalidPlanning
		}
	}
	if p.CasePack.Value.IsZero() {
		return ErrInvalidPlanning
	}
	if cmp, err := p.MOQ.Value.Cmp(p.MaxOrder.Value); err != nil || cmp > 0 {
		return ErrInvalidPlanning
	}
	return nil
}

type RecommendationStatus string

const (
	RecommendationProposed RecommendationStatus = "proposed"
	RecommendationAccepted RecommendationStatus = "accepted"
	RecommendationRejected RecommendationStatus = "rejected"
	RecommendationDeferred RecommendationStatus = "deferred"
	RecommendationOnHold   RecommendationStatus = "on_hold"
)

func (s RecommendationStatus) Valid() bool {
	return s == RecommendationProposed || s == RecommendationAccepted || s == RecommendationRejected || s == RecommendationDeferred || s == RecommendationOnHold
}

// ReplenishmentRecommendation is a derived decision candidate. It is not a
// PurchaseOrder and cannot submit one without the procurement boundary.
type ReplenishmentRecommendation struct {
	ID                  string
	RunID               string
	InputDigest         string
	OrganizationID      string
	WorkspaceID         string
	Grain               PlanningGrain
	SupplierOfferID     string
	RecommendedQuantity domain.Quantity
	ExpectedReceiptDays int
	RiskReductionBPS    int64
	ReasonCodes         []string
	EligibleMode        OperatingMode
	Status              RecommendationStatus
	Version             int64
	CreatedAt           time.Time
}

func (r ReplenishmentRecommendation) Validate() error {
	if !planningReferencePattern.MatchString(r.ID) || !planningReferencePattern.MatchString(r.RunID) || !hexDigestPlanning(r.InputDigest) || !planningReferencePattern.MatchString(r.OrganizationID) || !planningReferencePattern.MatchString(r.WorkspaceID) || r.Grain.Validate() != nil || !planningReferencePattern.MatchString(r.SupplierOfferID) || r.RecommendedQuantity.Validate() != nil || r.ExpectedReceiptDays < 1 || r.ExpectedReceiptDays > 366 || r.RiskReductionBPS < 0 || r.RiskReductionBPS > 10_000 || len(r.ReasonCodes) == 0 || len(r.ReasonCodes) > 32 || !r.EligibleMode.Valid() || !r.Status.Valid() || r.Version < 1 || !utcPlanning(r.CreatedAt) {
		return ErrInvalidPlanning
	}
	for _, reason := range r.ReasonCodes {
		if !planningReasonPattern.MatchString(reason) {
			return ErrInvalidPlanning
		}
	}
	return nil
}

// PurchasePlan links a recommendation to the existing procurement lifecycle.
// It intentionally models only a draft/decision boundary; sending remains a
// separate approved operation.
type PurchasePlan struct {
	ID               string
	RecommendationID string
	SupplierOfferID  string
	Mode             OperatingMode
	Status           string
	Quantity         domain.Quantity
	EstimatedCost    domain.Money
	IdempotencyKey   string
	ApprovalRequired bool
	KillSwitchActive bool
	CreatedAt        time.Time
	Version          int64
}

func (p PurchasePlan) Validate() error {
	if !planningReferencePattern.MatchString(p.ID) || !planningReferencePattern.MatchString(p.RecommendationID) || !planningReferencePattern.MatchString(p.SupplierOfferID) || !p.Mode.Valid() || (p.Status != "draft" && p.Status != "approved" && p.Status != "submitted" && p.Status != "unknown") || p.Quantity.Validate() != nil || p.EstimatedCost.Validate() != nil || !planningReferencePattern.MatchString(p.IdempotencyKey) || !utcPlanning(p.CreatedAt) || p.Version < 1 {
		return ErrInvalidPlanning
	}
	if p.Mode == AutoSubmit && (!p.ApprovalRequired || p.KillSwitchActive) {
		return ErrModeNotAllowed
	}
	return nil
}

// ForecastConfig controls the deterministic baseline. No model files or user
// code are loaded by this API.
type ForecastConfig struct {
	PeriodDays       int
	MinimumSamples   int
	AlgorithmVersion string
}

func (c ForecastConfig) Validate() error {
	if c.PeriodDays < 1 || c.PeriodDays > 31 || c.MinimumSamples < 1 || c.MinimumSamples > 365 || !planningReferencePattern.MatchString(c.AlgorithmVersion) {
		return ErrInvalidPlanning
	}
	return nil
}

// DigestObservations creates the immutable, provider-neutral input fingerprint.
func DigestObservations(observations []DemandObservation) (string, error) {
	cp := append([]DemandObservation(nil), observations...)
	for _, observation := range cp {
		if err := observation.Validate(); err != nil {
			return "", err
		}
	}
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Grain.key() != cp[j].Grain.key() {
			return cp[i].Grain.key() < cp[j].Grain.key()
		}
		if !cp[i].BucketStart.Equal(cp[j].BucketStart) {
			return cp[i].BucketStart.Before(cp[j].BucketStart)
		}
		return cp[i].ID < cp[j].ID
	})
	h := sha256.New()
	for _, observation := range cp {
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s|%s\n", observation.ID, observation.Grain.key(), observation.BucketStart.Format(time.RFC3339Nano), observation.ObservedAt.Format(time.RFC3339Nano), observation.Quantity.Value.String(), observation.Quantity.Unit.String(), observation.Returns.Value.String(), observation.Source)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// BuildForecast implements the first approved baseline: the latest normalized
// daily demand is carried forward, while p90 is the observed historical max.
// This seasonal-naive-like baseline is exact, deterministic and explainable.
func BuildForecast(run ForecastRun, observations []DemandObservation, config ForecastConfig) ([]ForecastPoint, error) {
	if config.Validate() != nil || run.Validate() != nil {
		return nil, ErrInvalidPlanning
	}
	if len(observations) == 0 {
		return nil, ErrInsufficientData
	}
	if config.AlgorithmVersion != run.AlgorithmVersion {
		return nil, ErrPlanningConflict
	}
	digest, err := DigestObservations(observations)
	if err != nil || digest != run.InputDigest {
		return nil, ErrPlanningConflict
	}
	groups := make(map[string][]DemandObservation)
	for _, observation := range observations {
		groups[observation.Grain.key()] = append(groups[observation.Grain.key()], observation)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	points := make([]ForecastPoint, 0, len(keys)*((run.HorizonDays+config.PeriodDays-1)/config.PeriodDays))
	for _, key := range keys {
		series := groups[key]
		sort.Slice(series, func(i, j int) bool { return series[i].BucketStart.Before(series[j].BucketStart) })
		if len(series) < config.MinimumSamples {
			continue
		}
		latest, err := series[len(series)-1].NetDemand()
		if err != nil {
			return nil, err
		}
		maxValue := latest
		for _, observation := range series {
			value, netErr := observation.NetDemand()
			if netErr != nil {
				return nil, netErr
			}
			if cmp, cmpErr := value.Value.Cmp(maxValue.Value); cmpErr != nil {
				return nil, cmpErr
			} else if cmp > 0 {
				maxValue = value
			}
		}
		confidence := int64(len(series)) * 1000
		if confidence > 9500 {
			confidence = 9500
		}
		for offset := 0; offset < run.HorizonDays; offset += config.PeriodDays {
			periodDays := config.PeriodDays
			if remaining := run.HorizonDays - offset; remaining < periodDays {
				periodDays = remaining
			}
			point := ForecastPoint{RunID: run.ID, InputDigest: run.InputDigest, Grain: series[0].Grain, PeriodStart: run.GeneratedAt.AddDate(0, 0, offset), PeriodDays: periodDays, DemandP50: latest, DemandP90: maxValue, ConfidenceBPS: confidence, SampleCount: int64(len(series)), Explanation: fmt.Sprintf("latest_observation_v1 samples=%d period_days=%d", len(series), periodDays), GeneratedAt: run.GeneratedAt, ValidUntil: run.ValidUntil}
			if err := point.Validate(); err != nil {
				return nil, err
			}
			points = append(points, point)
		}
	}
	if len(points) == 0 {
		return nil, ErrInsufficientData
	}
	return points, nil
}

// RoundToPack applies MOQ and case-pack rounding using integer arithmetic over
// a common decimal scale. It never uses binary floating point.
func RoundToPack(requested, moq, casePack domain.Quantity) (domain.Quantity, error) {
	if requested.Validate() != nil || moq.Validate() != nil || casePack.Validate() != nil || requested.Unit != moq.Unit || requested.Unit != casePack.Unit {
		return domain.Quantity{}, ErrUnitMismatch
	}
	for _, value := range []domain.Quantity{requested, moq, casePack} {
		if cmp, err := value.Value.Cmp(zeroDecimal()); err != nil || cmp < 0 {
			return domain.Quantity{}, ErrInvalidPlanning
		}
	}
	if casePack.Value.IsZero() {
		return domain.Quantity{}, ErrInvalidPlanning
	}
	if requested.Value.IsZero() {
		zero, _ := domain.NewDecimal(0, 0)
		return domain.NewQuantity(zero, requested.Unit)
	}
	scale := requested.Value.Scale()
	if moq.Value.Scale() > scale {
		scale = moq.Value.Scale()
	}
	if casePack.Value.Scale() > scale {
		scale = casePack.Value.Scale()
	}
	toScaled := func(value domain.Decimal) *big.Int {
		result := big.NewInt(value.Coefficient())
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale-value.Scale())), nil)
		return result.Mul(result, factor)
	}
	requestedScaled, moqScaled, packScaled := toScaled(requested.Value), toScaled(moq.Value), toScaled(casePack.Value)
	if requestedScaled.Cmp(moqScaled) < 0 {
		requestedScaled = moqScaled
	}
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(requestedScaled, packScaled, remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	result := new(big.Int).Mul(quotient, packScaled)
	if !result.IsInt64() {
		return domain.Quantity{}, ErrInvalidPlanning
	}
	value, err := domain.NewDecimal(result.Int64(), scale)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(value, requested.Unit)
}

// RecommendFromProjection creates a proposed recommendation and keeps all
// modes non-executing. Procurement workers must perform a second policy check.
func RecommendFromProjection(policy ReorderPolicy, projection StockProjection, run ForecastRun, now time.Time) (ReplenishmentRecommendation, error) {
	if policy.Validate() != nil || projection.Validate() != nil || run.Validate() != nil || !utcPlanning(now) || projection.RunID != run.ID || policy.OrganizationID != run.OrganizationID || policy.WorkspaceID != run.WorkspaceID {
		return ReplenishmentRecommendation{}, ErrInvalidPlanning
	}
	if policy.Grain != projection.Grain {
		return ReplenishmentRecommendation{}, ErrPlanningConflict
	}
	target, err := projection.ForecastDemand.Value.Add(policy.SafetyStock.Value)
	if err != nil {
		return ReplenishmentRecommendation{}, err
	}
	target, err = target.Sub(projection.ProjectedAvailable.Value)
	if err != nil {
		return ReplenishmentRecommendation{}, err
	}
	if cmp, cmpErr := target.Cmp(zeroDecimal()); cmpErr != nil {
		return ReplenishmentRecommendation{}, cmpErr
	} else if cmp < 0 {
		target = zeroDecimal()
	}
	requested, err := domain.NewQuantity(target, policy.SafetyStock.Unit)
	if err != nil {
		return ReplenishmentRecommendation{}, err
	}
	quantity, err := RoundToPack(requested, policy.MOQ, policy.CasePack)
	if err != nil {
		return ReplenishmentRecommendation{}, err
	}
	if cmp, cmpErr := quantity.Value.Cmp(policy.MaxOrder.Value); cmpErr != nil {
		return ReplenishmentRecommendation{}, cmpErr
	} else if cmp > 0 {
		return ReplenishmentRecommendation{ID: run.ID + ":" + policy.ID, RunID: run.ID, InputDigest: run.InputDigest, OrganizationID: run.OrganizationID, WorkspaceID: run.WorkspaceID, Grain: policy.Grain, SupplierOfferID: policy.SupplierOfferID, RecommendedQuantity: quantity, ExpectedReceiptDays: policy.TargetDays, RiskReductionBPS: 0, ReasonCodes: []string{"max_order_exceeded"}, EligibleMode: policy.Mode, Status: RecommendationOnHold, Version: 1, CreatedAt: now}, ErrRecommendationHold
	}
	reasons := []string{"projected_below_target"}
	if quantity.Value.IsZero() {
		reasons = []string{"within_target"}
	}
	return ReplenishmentRecommendation{ID: run.ID + ":" + policy.ID, RunID: run.ID, InputDigest: run.InputDigest, OrganizationID: run.OrganizationID, WorkspaceID: run.WorkspaceID, Grain: policy.Grain, SupplierOfferID: policy.SupplierOfferID, RecommendedQuantity: quantity, ExpectedReceiptDays: policy.TargetDays, RiskReductionBPS: 0, ReasonCodes: reasons, EligibleMode: policy.Mode, Status: RecommendationProposed, Version: 1, CreatedAt: now}, nil
}

func zeroDecimal() domain.Decimal {
	value, _ := domain.NewDecimal(0, 0)
	return value
}

func utcPlanning(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }

func hexDigestPlanning(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
