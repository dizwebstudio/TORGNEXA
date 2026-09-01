package api

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/replenishmentrepo"
	"github.com/torgnexa/torgnexa/internal/platform/replenishment"
)

const ReplenishmentPath = "/api/v1/replenishment"

type replenishmentAPI struct{ repository *replenishmentrepo.Repository }

type replenishmentQuantityInput struct {
	Coefficient int64  `json:"coefficient"`
	Scale       uint8  `json:"scale"`
	Unit        string `json:"unit"`
}

type replenishmentRunInput struct {
	RunID            string                     `json:"run_id"`
	AlgorithmVersion string                     `json:"algorithm_version"`
	HorizonDays      int                        `json:"horizon_days"`
	OfferID          string                     `json:"offer_id"`
	SKU              string                     `json:"sku"`
	WarehouseID      string                     `json:"warehouse_id"`
	SalesChannel     string                     `json:"sales_channel"`
	SupplierOfferID  string                     `json:"supplier_offer_id"`
	Unit             string                     `json:"unit"`
	Demand           replenishmentQuantityInput `json:"demand"`
	Returns          replenishmentQuantityInput `json:"returns"`
	Opening          replenishmentQuantityInput `json:"opening_available"`
	Inbound          replenishmentQuantityInput `json:"confirmed_inbound"`
	LeadTimeDays     int                        `json:"lead_time_days"`
}

func newReplenishmentRoutes(repository *replenishmentrepo.Repository) []ProtectedRoute {
	api := replenishmentAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ReplenishmentPath, Permission: "stock.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPost, Path: ReplenishmentPath, Permission: "stock.write", Handler: http.HandlerFunc(api.create)},
	}
}

func (a replenishmentAPI) create(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := replenishmentContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	if !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input replenishmentRunInput
	if decodeStrictJSON(r, &input) != nil || strings.TrimSpace(input.OfferID) == "" || strings.TrimSpace(input.SKU) == "" || strings.TrimSpace(input.WarehouseID) == "" || strings.TrimSpace(input.SupplierOfferID) == "" || input.HorizonDays < 1 || input.HorizonDays > 366 || input.LeadTimeDays < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = newApprovalID()
	}
	algorithm := strings.TrimSpace(input.AlgorithmVersion)
	if algorithm == "" {
		algorithm = "baseline.latest-net-demand.v1"
	}
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = strings.TrimSpace(input.Demand.Unit)
	}
	quantity, err := planningQuantity(input.Demand, unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid demand quantity")
		return
	}
	returns, err := planningQuantity(input.Returns, unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid returns quantity")
		return
	}
	opening, err := planningQuantity(input.Opening, unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid opening quantity")
		return
	}
	inbound, err := planningQuantity(input.Inbound, unit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid inbound quantity")
		return
	}
	now := time.Now().UTC()
	grain := replenishment.PlanningGrain{OfferID: strings.TrimSpace(input.OfferID), SKU: strings.TrimSpace(input.SKU), WarehouseID: strings.TrimSpace(input.WarehouseID), SalesChannel: strings.TrimSpace(input.SalesChannel)}
	observation := replenishment.DemandObservation{ID: runID + "-demand", Grain: grain, BucketStart: now.Truncate(24 * time.Hour), ObservedAt: now, Quantity: quantity, Returns: returns, Source: "api.replenishment"}
	run, err := replenishment.NewForecastRun(runID, scope.OrganizationID().String(), scope.WorkspaceID().String(), algorithm, input.HorizonDays, []replenishment.DemandObservation{observation}, now)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid planning run")
		return
	}
	run.Status = "completed"
	netDemand, err := observation.NetDemand()
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid demand observations")
		return
	}
	p90, err := inflateQuantity(netDemand, 20)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Demand exceeds numeric limits")
		return
	}
	point := replenishment.ForecastPoint{RunID: run.ID, InputDigest: run.InputDigest, Grain: grain, PeriodStart: now, PeriodDays: input.HorizonDays, DemandP50: netDemand, DemandP90: p90, ConfidenceBPS: 5000, SampleCount: 1, Explanation: "baseline: latest verified net demand; p90 adds a bounded 20% uncertainty buffer", GeneratedAt: now, ValidUntil: run.ValidUntil}
	projection, err := replenishment.ProjectStock(run.ID, grain, now, opening, inbound, netDemand)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid stock projection")
		return
	}
	quantityToOrder := projection.Shortfall
	reasons := []string{"baseline_latest_net_demand"}
	if !quantityToOrder.Value.IsZero() {
		reasons = append(reasons, "stockout_risk")
	} else {
		reasons = append(reasons, "stock_covered")
	}
	recommendation := replenishment.ReplenishmentRecommendation{ID: run.ID + "-recommendation-1", RunID: run.ID, InputDigest: run.InputDigest, OrganizationID: run.OrganizationID, WorkspaceID: run.WorkspaceID, Grain: grain, SupplierOfferID: strings.TrimSpace(input.SupplierOfferID), RecommendedQuantity: quantityToOrder, ExpectedReceiptDays: input.LeadTimeDays, RiskReductionBPS: boolBPS(projection.StockoutRisk), ReasonCodes: reasons, EligibleMode: replenishment.RecommendationOnly, Status: replenishment.RecommendationProposed, Version: 1, CreatedAt: now}
	record := replenishmentrepo.Record{Run: run, Points: []replenishment.ForecastPoint{point}, Projections: []replenishment.StockProjection{projection}, Recommendations: []replenishment.ReplenishmentRecommendation{recommendation}}
	mutation := replenishmentrepo.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), ActorID: boundedActorRef(principal.Subject), Source: "api.replenishment", CorrelationID: boundedActorRef(r.Header.Get("Idempotency-Key")), OccurredAt: now}
	if err := a.repository.Save(r.Context(), scope, record, mutation); err != nil {
		if errors.Is(err, replenishmentrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Planning run already exists with different input")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Planning run failed")
		return
	}
	writeJSON(w, http.StatusCreated, replenishmentRecordResponse(record))
}

func (a replenishmentAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, _, ok := replenishmentContext(w, r)
	if !ok || a.repository == nil {
		return
	}
	items, err := a.repository.List(r.Context(), scope, 50)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Planning runs unavailable")
		return
	}
	views := make([]any, 0, len(items))
	for _, item := range items {
		views = append(views, replenishmentRecordResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func replenishmentContext(w http.ResponseWriter, r *http.Request) (tenancy.Scope, Principal, bool) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !scopeOK || !principalOK || principal.Subject == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return tenancy.Scope{}, Principal{}, false
	}
	return scope, principal, true
}

func planningQuantity(input replenishmentQuantityInput, fallbackUnit string) (domain.Quantity, error) {
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = fallbackUnit
	}
	value, err := domain.NewDecimal(input.Coefficient, input.Scale)
	if err != nil {
		return domain.Quantity{}, err
	}
	unitCode, err := domain.NewUnitCode(unit)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(value, unitCode)
}

func inflateQuantity(value domain.Quantity, percent int64) (domain.Quantity, error) {
	coefficient := value.Value.Coefficient()
	if coefficient > math.MaxInt64/(100+percent) {
		return domain.Quantity{}, errors.New("quantity overflow")
	}
	valueDecimal, err := domain.NewDecimal(coefficient*(100+percent)/100, value.Value.Scale())
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.NewQuantity(valueDecimal, value.Unit)
}

func boolBPS(value bool) int64 {
	if value {
		return 10000
	}
	return 0
}

func replenishmentRecordResponse(record replenishmentrepo.Record) map[string]any {
	recommendations := make([]any, 0, len(record.Recommendations))
	for _, item := range record.Recommendations {
		recommendations = append(recommendations, map[string]any{"id": item.ID, "run_id": item.RunID, "offer_id": item.Grain.OfferID, "sku": item.Grain.SKU, "warehouse_id": item.Grain.WarehouseID, "sales_channel": item.Grain.SalesChannel, "supplier_offer_id": item.SupplierOfferID, "recommended_quantity": map[string]any{"coefficient": item.RecommendedQuantity.Value.Coefficient(), "scale": item.RecommendedQuantity.Value.Scale(), "unit": item.RecommendedQuantity.Unit.String()}, "expected_receipt_days": item.ExpectedReceiptDays, "risk_reduction_bps": item.RiskReductionBPS, "reason_codes": item.ReasonCodes, "eligible_mode": item.EligibleMode, "status": item.Status, "version": item.Version, "created_at": item.CreatedAt})
	}
	return map[string]any{"run": map[string]any{"id": record.Run.ID, "algorithm_version": record.Run.AlgorithmVersion, "input_digest": record.Run.InputDigest, "horizon_days": record.Run.HorizonDays, "generated_at": record.Run.GeneratedAt, "valid_until": record.Run.ValidUntil, "status": record.Run.Status, "quality": record.Run.Quality}, "recommendations": recommendations}
}
