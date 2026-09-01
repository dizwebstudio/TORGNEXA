// Package replenishmentrepo persists the derived forecast and replenishment
// projection. It deliberately has no method that mutates inventory or sends a
// purchase order.
package replenishmentrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/replenishment"
)

var (
	ErrInvalid  = errors.New("replenishment repository: invalid record")
	ErrConflict = errors.New("replenishment repository: run already exists")
	ErrNotFound = errors.New("replenishment repository: run not found")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Record is the bounded operator projection returned by the planning API.
type Record struct {
	Run             replenishment.ForecastRun
	Points          []replenishment.ForecastPoint
	Projections     []replenishment.StockProjection
	Recommendations []replenishment.ReplenishmentRecommendation
}

// Mutation is the audit/outbox metadata for a planning run.
type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID string
	OccurredAt                                       time.Time
}

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("replenishment repository: database required")
	}
	return &Repository{db: db}, nil
}

// Save atomically stores a completed derived run and its operator-visible
// outputs. Replaying the same run ID is safe only when its digest matches.
func (r *Repository) Save(ctx context.Context, scope tenancy.Scope, record Record, mutation Mutation) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || record.Run.Validate() != nil || record.Run.OrganizationID != scope.OrganizationID().String() || record.Run.WorkspaceID != scope.WorkspaceID().String() || mutation.EventID == "" || mutation.AuditID == "" || mutation.ActorID == "" || mutation.Source == "" || mutation.CorrelationID == "" || mutation.OccurredAt.IsZero() || mutation.OccurredAt.Location() != time.UTC {
		return ErrInvalid
	}
	for _, point := range record.Points {
		if point.Validate() != nil || point.RunID != record.Run.ID || point.InputDigest != record.Run.InputDigest {
			return ErrInvalid
		}
	}
	for _, projection := range record.Projections {
		if projection.Validate() != nil || projection.RunID != record.Run.ID {
			return ErrInvalid
		}
	}
	for _, recommendation := range record.Recommendations {
		if recommendation.Validate() != nil || recommendation.RunID != record.Run.ID || recommendation.InputDigest != record.Run.InputDigest {
			return ErrInvalid
		}
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var existingDigest string
		err := tx.QueryRowContext(ctx, `SELECT input_digest FROM replenishment_forecast_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), record.Run.ID).Scan(&existingDigest)
		if err == nil {
			if existingDigest == record.Run.InputDigest {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		quality := record.Run.Quality
		if _, err := tx.ExecContext(ctx, `INSERT INTO replenishment_forecast_runs(organization_id,workspace_id,run_id,algorithm_version,input_digest,horizon_days,generated_at,valid_until,status,quality_status,freshness_seconds,coverage_bps,sample_count,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$7,$7)`, record.Run.OrganizationID, record.Run.WorkspaceID, record.Run.ID, record.Run.AlgorithmVersion, record.Run.InputDigest, record.Run.HorizonDays, record.Run.GeneratedAt, record.Run.ValidUntil, record.Run.Status, quality.Status, quality.FreshnessSeconds, quality.CoverageBPS, quality.SampleCount, record.Run.Version); err != nil {
			return fmt.Errorf("insert forecast run: %w", err)
		}
		for index, point := range record.Points {
			if _, err := tx.ExecContext(ctx, `INSERT INTO replenishment_forecast_points(organization_id,workspace_id,point_id,run_id,offer_id,sku,warehouse_id,sales_channel,period_start,period_days,unit,demand_p50_coefficient,demand_p50_scale,demand_p90_coefficient,demand_p90_scale,confidence_bps,sample_count,explanation,generated_at,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), fmt.Sprintf("%s-point-%d", record.Run.ID, index+1), point.RunID, point.Grain.OfferID, point.Grain.SKU, point.Grain.WarehouseID, point.Grain.SalesChannel, point.PeriodStart, point.PeriodDays, point.DemandP50.Unit.String(), point.DemandP50.Value.Coefficient(), point.DemandP50.Value.Scale(), point.DemandP90.Value.Coefficient(), point.DemandP90.Value.Scale(), point.ConfidenceBPS, point.SampleCount, point.Explanation, point.GeneratedAt, point.ValidUntil); err != nil {
				return fmt.Errorf("insert forecast point: %w", err)
			}
		}
		for index, projection := range record.Projections {
			if _, err := tx.ExecContext(ctx, `INSERT INTO replenishment_stock_projections(organization_id,workspace_id,projection_id,run_id,offer_id,sku,warehouse_id,sales_channel,period_start,unit,opening_coefficient,opening_scale,inbound_coefficient,inbound_scale,demand_coefficient,demand_scale,projected_coefficient,projected_scale,shortfall_coefficient,shortfall_scale,days_of_supply_coefficient,days_of_supply_scale,stockout_risk,overstock_risk,explanation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), fmt.Sprintf("%s-projection-%d", record.Run.ID, index+1), projection.RunID, projection.Grain.OfferID, projection.Grain.SKU, projection.Grain.WarehouseID, projection.Grain.SalesChannel, projection.PeriodStart, projection.OpeningAvailable.Unit.String(), projection.OpeningAvailable.Value.Coefficient(), projection.OpeningAvailable.Value.Scale(), projection.ConfirmedInbound.Value.Coefficient(), projection.ConfirmedInbound.Value.Scale(), projection.ForecastDemand.Value.Coefficient(), projection.ForecastDemand.Value.Scale(), projection.ProjectedAvailable.Value.Coefficient(), projection.ProjectedAvailable.Value.Scale(), projection.Shortfall.Value.Coefficient(), projection.Shortfall.Value.Scale(), projection.DaysOfSupply.Coefficient(), projection.DaysOfSupply.Scale(), projection.StockoutRisk, projection.OverstockRisk, projection.Explanation); err != nil {
				return fmt.Errorf("insert stock projection: %w", err)
			}
		}
		for _, recommendation := range record.Recommendations {
			reasons, err := json.Marshal(recommendation.ReasonCodes)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO replenishment_recommendations(organization_id,workspace_id,recommendation_id,run_id,input_digest,offer_id,sku,warehouse_id,sales_channel,supplier_offer_id,quantity_coefficient,quantity_scale,unit,expected_receipt_days,risk_reduction_bps,reason_codes,eligible_mode,status,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18,$19,$20)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), recommendation.ID, recommendation.RunID, recommendation.InputDigest, recommendation.Grain.OfferID, recommendation.Grain.SKU, recommendation.Grain.WarehouseID, recommendation.Grain.SalesChannel, recommendation.SupplierOfferID, recommendation.RecommendedQuantity.Value.Coefficient(), recommendation.RecommendedQuantity.Value.Scale(), recommendation.RecommendedQuantity.Unit.String(), recommendation.ExpectedReceiptDays, recommendation.RiskReductionBPS, string(reasons), recommendation.EligibleMode, recommendation.Status, recommendation.Version, recommendation.CreatedAt); err != nil {
				return fmt.Errorf("insert replenishment recommendation: %w", err)
			}
		}
		scopeCore, err := tenancy.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		if err := auditrepo.AppendTransaction(ctx, tx, scopeCore, audit.Record{ID: mutation.AuditID, OrganizationID: scopeCore.OrganizationID(), WorkspaceID: scopeCore.WorkspaceID(), ActorID: mutation.ActorID, Source: mutation.Source, Action: "replenishment.run.completed", ResourceType: "forecast_run", ResourceID: record.Run.ID, CorrelationID: mutation.CorrelationID, Risk: audit.RiskRead, Summary: audit.Summary{"algorithm_version": record.Run.AlgorithmVersion, "recommendation_count": len(record.Recommendations), "quality": string(quality.Status)}, CreatedAt: mutation.OccurredAt}); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"run_id": record.Run.ID, "status": record.Run.Status, "input_digest": record.Run.InputDigest, "recommendation_count": len(record.Recommendations), "version": record.Run.Version})
		if err != nil {
			return err
		}
		typ, err := eventbus.ParseEventType("commerce.replenishment.run_completed.v1")
		if err != nil {
			return err
		}
		instant, err := timeToInstant(mutation.OccurredAt)
		if err != nil {
			return err
		}
		event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: instant, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: "forecast_run", EntityID: record.Run.ID, Source: mutation.Source, CorrelationID: mutation.CorrelationID, ActorID: mutation.ActorID, Data: payload}
		if err := event.Validate(); err != nil {
			return err
		}
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		return enqueuer.Enqueue(ctx, event)
	})
}

// List returns the latest bounded planning runs with their recommendations.
func (r *Repository) List(ctx context.Context, scope tenancy.Scope, limit int) ([]Record, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	var result []Record
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT run_id,organization_id,workspace_id,algorithm_version,input_digest,horizon_days,generated_at,valid_until,status,quality_status,freshness_seconds,coverage_bps,sample_count,version FROM replenishment_forecast_runs WHERE organization_id=$1 AND workspace_id=$2 ORDER BY generated_at DESC,run_id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run replenishment.ForecastRun
			var qualityStatus string
			if err := rows.Scan(&run.ID, &run.OrganizationID, &run.WorkspaceID, &run.AlgorithmVersion, &run.InputDigest, &run.HorizonDays, &run.GeneratedAt, &run.ValidUntil, &run.Status, &qualityStatus, &run.Quality.FreshnessSeconds, &run.Quality.CoverageBPS, &run.Quality.SampleCount, &run.Version); err != nil {
				return err
			}
			run.Quality.Status = replenishment.PlanningStatus(qualityStatus)
			result = append(result, Record{Run: run})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for index := range result {
			rows, err := tx.QueryContext(ctx, `SELECT recommendation_id,run_id,input_digest,offer_id,sku,warehouse_id,sales_channel,supplier_offer_id,quantity_coefficient,quantity_scale,unit,expected_receipt_days,risk_reduction_bps,reason_codes,eligible_mode,status,version,created_at FROM replenishment_recommendations WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 ORDER BY recommendation_id LIMIT 200`, scope.OrganizationID().String(), scope.WorkspaceID().String(), result[index].Run.ID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, runID, digest, offerID, sku, warehouseID, channel, supplierOfferID, unit, eligibleMode, status string
				var coefficient int64
				var scale uint8
				var expectedDays int
				var risk int64
				var reasonsJSON []byte
				var version int64
				var created time.Time
				if err := rows.Scan(&id, &runID, &digest, &offerID, &sku, &warehouseID, &channel, &supplierOfferID, &coefficient, &scale, &unit, &expectedDays, &risk, &reasonsJSON, &eligibleMode, &status, &version, &created); err != nil {
					rows.Close()
					return err
				}
				unitCode, err := domainUnit(unit)
				if err != nil {
					rows.Close()
					return err
				}
				decimal, err := domainDecimal(coefficient, scale)
				if err != nil {
					rows.Close()
					return err
				}
				quantity, err := domainQuantity(decimal, unitCode)
				if err != nil {
					rows.Close()
					return err
				}
				var reasons []string
				if err := json.Unmarshal(reasonsJSON, &reasons); err != nil {
					rows.Close()
					return err
				}
				result[index].Recommendations = append(result[index].Recommendations, replenishment.ReplenishmentRecommendation{ID: id, RunID: runID, InputDigest: digest, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), Grain: replenishment.PlanningGrain{OfferID: offerID, SKU: sku, WarehouseID: warehouseID, SalesChannel: channel}, SupplierOfferID: supplierOfferID, RecommendedQuantity: quantity, ExpectedReceiptDays: expectedDays, RiskReductionBPS: risk, ReasonCodes: reasons, EligibleMode: replenishment.OperatingMode(eligibleMode), Status: replenishment.RecommendationStatus(status), Version: version, CreatedAt: created})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return err
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Small constructors keep the conversion from SQL primitives in one place.
func domainUnit(value string) (domain.UnitCode, error) { return domain.NewUnitCode(value) }
func domainDecimal(coefficient int64, scale uint8) (domain.Decimal, error) {
	return domain.NewDecimal(coefficient, scale)
}
func domainQuantity(value domain.Decimal, unit domain.UnitCode) (domain.Quantity, error) {
	return domain.NewQuantity(value, unit)
}
func timeToInstant(value time.Time) (domain.UTCInstant, error) { return domain.NewUTCInstant(value) }
